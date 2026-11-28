#!/usr/bin/env bash
set -euo pipefail

IMAGE="${1:-raterudder:smoke}"
EMULATOR_IMAGE="${FIRESTORE_EMULATOR_IMAGE:-gcr.io/google.com/cloudsdktool/google-cloud-cli:emulators}"

RUN_ID="$$-${RANDOM}"
NETWORK_NAME="raterudder-smoke-${RUN_ID}"
EMULATOR_CONTAINER="raterudder-firestore-${RUN_ID}"
APP_CONTAINER="raterudder-app-${RUN_ID}"
WORK_DIR="$(mktemp -d)"
SECRET_DIR="${WORK_DIR}/secrets"

dump_diagnostics() {
  echo "--- Smoke check failed; dumping container diagnostics ---" >&2
  if docker ps -a --format '{{.Names}}' | grep -Fxq "${APP_CONTAINER}"; then
    echo "=== ${APP_CONTAINER} state ===" >&2
    docker inspect -f 'status={{.State.Status}} exitCode={{.State.ExitCode}} error={{.State.Error}}' "${APP_CONTAINER}" >&2 || true
    echo "=== ${APP_CONTAINER} logs ===" >&2
    docker logs "${APP_CONTAINER}" >&2 || true
  fi
  if docker ps -a --format '{{.Names}}' | grep -Fxq "${EMULATOR_CONTAINER}"; then
    echo "=== ${EMULATOR_CONTAINER} logs ===" >&2
    docker logs "${EMULATOR_CONTAINER}" >&2 || true
  fi
}

cleanup() {
  local exit_code=$?
  if [ "${exit_code}" -ne 0 ]; then
    dump_diagnostics
  fi
  docker rm -f "${APP_CONTAINER}" >/dev/null 2>&1 || true
  docker rm -f "${EMULATOR_CONTAINER}" >/dev/null 2>&1 || true
  docker network rm "${NETWORK_NAME}" >/dev/null 2>&1 || true
  rm -rf "${WORK_DIR}"
}
trap cleanup EXIT

# Prepare 0755 secret mount directory and 0644 config.json so both root (0:0)
# and distroless :nonroot (65532:65532) containers can read CONFIG_JSON_FILE.
mkdir -p "${SECRET_DIR}"
chmod 0755 "${WORK_DIR}" "${SECRET_DIR}"

SESSION_KEY="$(printf 'FAKE_SESSION_%019d' 0)"
CREDENTIALS_KEY="$(printf 'FAKE_CREDKEY_%019d' 0)"

cat > "${SECRET_DIR}/config.json" <<EOF
{
  "session-encryption-key": "${SESSION_KEY}"
}
EOF
chmod 0644 "${SECRET_DIR}/config.json"

echo "Creating isolated Docker network ${NETWORK_NAME}..."
docker network create "${NETWORK_NAME}" >/dev/null

echo "Starting Firestore emulator (${EMULATOR_IMAGE})..."
docker run -d \
  --name "${EMULATOR_CONTAINER}" \
  --network "${NETWORK_NAME}" \
  -p 127.0.0.1::8087 \
  "${EMULATOR_IMAGE}" \
  gcloud emulators firestore start --host-port=0.0.0.0:8087 --quiet >/dev/null

EMULATOR_HOST_ADDR="$(docker port "${EMULATOR_CONTAINER}" 8087/tcp | head -n 1)"
if [ -z "${EMULATOR_HOST_ADDR}" ]; then
  echo "Failed to resolve host port for Firestore emulator" >&2
  exit 1
fi

count=0
until curl -fsS --max-time 2 "http://${EMULATOR_HOST_ADDR}/" >/dev/null 2>&1; do
  if [ "$(docker inspect -f '{{.State.Running}}' "${EMULATOR_CONTAINER}" 2>/dev/null || echo false)" != "true" ]; then
    echo "Firestore emulator container exited unexpectedly" >&2
    exit 1
  fi
  sleep 1
  count=$((count + 1))
  if [ "${count}" -ge 60 ]; then
    echo "Timed out waiting for Firestore emulator at http://${EMULATOR_HOST_ADDR}/" >&2
    exit 1
  fi
done
echo "Firestore emulator ready at ${EMULATOR_HOST_ADDR}."

echo "Starting RateRudder container (${IMAGE})..."
docker run -d \
  --name "${APP_CONTAINER}" \
  --network "${NETWORK_NAME}" \
  -p 127.0.0.1::8080 \
  -e CONFIG_JSON_FILE="/secrets/config.json" \
  -e CREDENTIALS_ENCRYPTION_KEY="${CREDENTIALS_KEY}" \
  -v "${SECRET_DIR}:/secrets:ro" \
  "${IMAGE}" \
  --http-listen=:8080 \
  --firestore-project-id=demo-smoke \
  --firestore-emulator="${EMULATOR_CONTAINER}:8087" >/dev/null

APP_HOST_ADDR="$(docker port "${APP_CONTAINER}" 8080/tcp | head -n 1)"
if [ -z "${APP_HOST_ADDR}" ]; then
  echo "Failed to resolve host port for RateRudder container" >&2
  exit 1
fi
BASE_URL="http://${APP_HOST_ADDR}"

request() {
  local path="$1"
  local headers_file="$2"
  local body_file="$3"
  local status
  status="$(curl -sS --compressed --max-time 15 -D "${headers_file}" -o "${body_file}" -w '%{http_code}' "${BASE_URL}${path}")"
  tr -d '\r' < "${headers_file}" > "${headers_file}.clean"
  mv "${headers_file}.clean" "${headers_file}"
  printf '%s' "${status}"
}

assert_status() {
  local path="$1"
  local expected="$2"
  local actual="$3"
  local body_file="$4"
  if [ "${actual}" != "${expected}" ]; then
    echo "Expected HTTP ${expected} for ${path}, got ${actual}" >&2
    cat "${body_file}" >&2 || true
    exit 1
  fi
}

# 1. Wait for startup and verify 200 on /healthz with Firestore emulator
echo "Waiting for RateRudder /healthz at ${BASE_URL}/healthz..."
HEALTH_HEADERS="${WORK_DIR}/health.headers"
HEALTH_BODY="${WORK_DIR}/health.body"
count=0
while true; do
  if [ "$(docker inspect -f '{{.State.Running}}' "${APP_CONTAINER}" 2>/dev/null || echo false)" != "true" ]; then
    echo "RateRudder container exited during startup" >&2
    exit 1
  fi
  health_status="$(curl -s --max-time 2 -D "${HEALTH_HEADERS}" -o "${HEALTH_BODY}" -w '%{http_code}' "${BASE_URL}/healthz" || true)"
  if [ "${health_status}" = "200" ]; then
    break
  fi
  sleep 1
  count=$((count + 1))
  if [ "${count}" -ge 30 ]; then
    echo "Timed out waiting for HTTP 200 from /healthz (last status: ${health_status})" >&2
    exit 1
  fi
done

if [ "$(cat "${HEALTH_BODY}")" != "ok" ]; then
  echo "Unexpected /healthz response body: $(cat "${HEALTH_BODY}")" >&2
  exit 1
fi
echo "Verified /healthz -> 200 ok"

# 2. Verify GET /, security headers (Content-Security-Policy & Reporting-Endpoints), and hashed JS bundle
INDEX_HEADERS="${WORK_DIR}/index.headers"
INDEX_BODY="${WORK_DIR}/index.body"
status="$(request "/" "${INDEX_HEADERS}" "${INDEX_BODY}")"
assert_status "/" "200" "${status}" "${INDEX_BODY}"

if ! grep -qi '^Content-Security-Policy:.*default-src '\''self'\''' "${INDEX_HEADERS}"; then
  echo "Missing or invalid Content-Security-Policy header on /" >&2
  cat "${INDEX_HEADERS}" >&2
  exit 1
fi
if ! grep -qi '^Content-Security-Policy:.*report-to browser-reports' "${INDEX_HEADERS}"; then
  echo "Content-Security-Policy header missing report-to directive on /" >&2
  cat "${INDEX_HEADERS}" >&2
  exit 1
fi
if ! grep -qi '^Reporting-Endpoints:.*browser-reports="/api/report/browser"' "${INDEX_HEADERS}"; then
  echo "Missing or invalid Reporting-Endpoints header on /" >&2
  cat "${INDEX_HEADERS}" >&2
  exit 1
fi
echo "Verified /, Content-Security-Policy, and Reporting-Endpoints headers"

BUNDLE_PATH="$(grep -oE '/assets/[^"'\'' >]+\.js' "${INDEX_BODY}" | head -n 1 || true)"
if [ -z "${BUNDLE_PATH}" ]; then
  echo "Could not find hashed JS bundle reference (/assets/*.js) in / HTML" >&2
  cat "${INDEX_BODY}" >&2
  exit 1
fi

BUNDLE_HEADERS="${WORK_DIR}/bundle.headers"
BUNDLE_BODY="${WORK_DIR}/bundle.body"
status="$(request "${BUNDLE_PATH}" "${BUNDLE_HEADERS}" "${BUNDLE_BODY}")"
assert_status "${BUNDLE_PATH}" "200" "${status}" "${BUNDLE_BODY}"
if [ ! -s "${BUNDLE_BODY}" ]; then
  echo "Hashed JS bundle ${BUNDLE_PATH} returned an empty body" >&2
  exit 1
fi
if ! grep -qi '^Content-Type:.*javascript' "${BUNDLE_HEADERS}"; then
  echo "Expected JavaScript Content-Type for ${BUNDLE_PATH}" >&2
  cat "${BUNDLE_HEADERS}" >&2
  exit 1
fi
echo "Verified hashed JS bundle ${BUNDLE_PATH} -> 200"

# 3. Verify GET /how-it-works (prerendered SEO subpage)
SEO_HEADERS="${WORK_DIR}/seo.headers"
SEO_BODY="${WORK_DIR}/seo.body"
status="$(request "/how-it-works" "${SEO_HEADERS}" "${SEO_BODY}")"
assert_status "/how-it-works" "200" "${status}" "${SEO_BODY}"
if ! grep -q 'How SmartPlan Works' "${SEO_BODY}"; then
  echo "Expected SEO subpage content in /how-it-works response" >&2
  exit 1
fi
echo "Verified /how-it-works -> 200"

# 4. Verify GET /sw.js (service worker)
SW_HEADERS="${WORK_DIR}/sw.headers"
SW_BODY="${WORK_DIR}/sw.body"
status="$(request "/sw.js" "${SW_HEADERS}" "${SW_BODY}")"
assert_status "/sw.js" "200" "${status}" "${SW_BODY}"
if [ ! -s "${SW_BODY}" ]; then
  echo "/sw.js returned an empty body" >&2
  exit 1
fi
if ! grep -qi '^Content-Type:.*javascript' "${SW_HEADERS}"; then
  echo "Expected JavaScript Content-Type for /sw.js" >&2
  cat "${SW_HEADERS}" >&2
  exit 1
fi
echo "Verified /sw.js -> 200"

# 5. Verify GET /api/list/utilities
UTIL_HEADERS="${WORK_DIR}/utilities.headers"
UTIL_BODY="${WORK_DIR}/utilities.body"
status="$(request "/api/list/utilities" "${UTIL_HEADERS}" "${UTIL_BODY}")"
assert_status "/api/list/utilities" "200" "${status}" "${UTIL_BODY}"
if ! grep -qi '^Content-Type:.*application/json' "${UTIL_HEADERS}"; then
  echo "Expected application/json Content-Type for /api/list/utilities" >&2
  cat "${UTIL_HEADERS}" >&2
  exit 1
fi
if ! grep -q '"comed"' "${UTIL_BODY}"; then
  echo "Expected utility providers JSON array in /api/list/utilities response" >&2
  cat "${UTIL_BODY}" >&2
  exit 1
fi
echo "Verified /api/list/utilities -> 200"

# 6. Verify 404 on /assets/nonexistent-chunk.js (no SPA index.html fallback)
MISSING_HEADERS="${WORK_DIR}/missing.headers"
MISSING_BODY="${WORK_DIR}/missing.body"
status="$(request "/assets/nonexistent-chunk.js" "${MISSING_HEADERS}" "${MISSING_BODY}")"
assert_status "/assets/nonexistent-chunk.js" "404" "${status}" "${MISSING_BODY}"
echo "Verified /assets/nonexistent-chunk.js -> 404"

# 7. Stop Firestore emulator and verify /healthz returns 503 without Firestore
echo "Stopping Firestore emulator to verify degraded /healthz -> 503..."
docker rm -f "${EMULATOR_CONTAINER}" >/dev/null

UNHEALTHY_HEADERS="${WORK_DIR}/unhealthy.headers"
UNHEALTHY_BODY="${WORK_DIR}/unhealthy.body"
status="$(request "/healthz" "${UNHEALTHY_HEADERS}" "${UNHEALTHY_BODY}")"
assert_status "/healthz (without Firestore)" "503" "${status}" "${UNHEALTHY_BODY}"
echo "Verified /healthz without Firestore -> 503"

echo "All container smoke checks passed for ${IMAGE}."
