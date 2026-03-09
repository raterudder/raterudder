# RateRudder

RateRudder is an intelligent home energy management system designed to optimize the usage of Energy Storage Systems (ESS) like FranklinWH based on real-time/TOU electricity pricing. It automates the charging and discharging of batteries to maximize savings and efficiency.

## Architecture

The project is structured as follows:

- **`cmd/raterudder`**: The main entry point and orchestrator.
- **`pkg`**: Core backend logic.
    - **`controller`**: Decision-making logic for ESS control.
    - **`ess`**: Interfaces and implementations for ESS (currently supports FranklinWH).
    - **`server`**: HTTP API server for the web dashboard and triggered updates.
    - **`storage`**: Persistence layer (currently supports Google Cloud Firestore).
    - **`utility`**: Electricity pricing fetchers (ComEd, Ameren & PJM/MISO).
    - **`weather`**: Weather status forecasts and geocoding.
- **`web`**: A React + TypeScript + Vite single-page application for the frontend dashboard.
- **`tf`**: Terraform configuration for provisioning infrastructure on Google Cloud.

## Getting Started

### Prerequisites

- Go 1.25+
- Node.js & npm (for web development)
- Google Cloud Project with Firestore enabled
- FranklinWH Account Credentials

### Installation

1.  **Clone the repository:**

    ```bash
    git clone https://github.com/raterudder/raterudder.git
    cd raterudder
    ```

2.  **Build the Web App (Optional if running dev server):**

    ```bash
    cd web
    npm install
    npm run build
    cd ..
    ```

3.  **Build the Go Binary:**

    ```bash
    go build ./cmd/raterudder
    ```

### Usage

Run the binary with the necessary flags.

```bash
./raterudder --help
```

### Configuration Flags

The application uses command-line flags for configuration.

#### General / Server
- `--http-listen`: HTTP server listen address (default `:8080`).
- `--dev-proxy`: Address of the dev server (e.g., `http://localhost:5173`).
- `--update-specific-email`: Email requirement for authenticating calls to `/api/update`.
- `--admin-emails`: Comma-delimited list of email addresses allowed to update settings via IAP.
- `--oidc-audience`: Expected audience for OIDC token validation.
- `--oidc-audiences`: JSON map of provider (`google`/`apple`) to audience/client ID.
- `--update-specific-audience`: Google-specific legacy audience to validate for `/api/update`.
- `--single-site`: Enable single-site mode (disables siteID requirement), for simple single-user deployments.
- `--show-hidden`: Expose hidden providers in lists via the API.
- `--credentials-encryption-key`: Key for encrypting credentials (must be 32 characters).
- `--release`: Release environment (`production` or `staging`).
- `--web-cache-duration`: Duration to cache web files (e.g., `1h`, `5m`). `0` means no cache.

#### Utility & Weather
- `--comed-api-url`: URL for the ComEd Hourly Pricing API.
- `--pjm-api-url`: URL for the PJM API (Day-ahead pricing).
- `--pjm-api-key`: API Key for PJM Data Miner 2 (optional, enabled day-ahead lookups).
- `--miso-api-url`: URL for the MISO API.
- `--weather-geocoding-url`: Open-Meteo geocoding API URL.
- `--weather-forecast-url`: Open-Meteo forecast API URL.

#### Storage (Firestore)
- `--storage-provider`: Provider to use (default `firestore`).
- `--firestore-project-id`: Google Cloud Project ID for Firestore.
- `--firestore-database`: Google Cloud Firestore Database.
- `--firestore-emulator`: Use Firestore emulator.

## Development

### Running Locally

To run the full stack locally:

1.  **Start Firestore Emulator:**

    ```bash
    gcloud emulators firestore start --host-port=127.0.0.1:8087
    ```

2.  **Start Web Dev Server:**

    ```bash
    cd web
    npm run dev
    ```

3.  **Run Go Backend:**

    ```bash
    export FIRESTORE_EMULATOR_HOST=127.0.0.1:8087
    go run ./cmd/raterudder \
      --dev-proxy=http://localhost:5173 \
      --credentials-encryption-key=YOUR_32_CHAR_LONG_SECRET_KEY_HERE
    ```

### Running Tests

To run all Go tests:

```bash
go test ./...
```

Firestore integration tests will automatically use the emulator if `FIRESTORE_EMULATOR_HOST` is set or default to `127.0.0.1:8087`.

## Deployment

The `tf` directory contains Terraform code to deploy the application to Google Cloud Platform. It sets up:
- **Cloud Run**: Hosts the Go server (which serves the embedded React app).
- **Cloud Scheduler**: Triggers the `/api/update` endpoint periodically.
- **Firestore**: Database for settings, history, and actions.
- **Secret Manager**: Securely stores credentials.
