package controller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	syntheticSiteIDPattern = regexp.MustCompile(`^site[0-9]+$`)
	emailPattern           = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
)

var forbiddenFixtureJSONKeys = map[string]struct{}{
	"credentials":    {},
	"franklin":       {},
	"tesla":          {},
	"enphase":        {},
	"authcode":       {},
	"accesstoken":    {},
	"refreshtoken":   {},
	"energysiteid":   {},
	"serialnumber":   {},
	"username":       {},
	"password":       {},
	"md5password":    {},
	"gatewayid":      {},
	"token":          {},
	"sessionid":      {},
	"managertoken":   {},
	"systemid":       {},
	"userid":         {},
	"privatekey":     {},
	"private_key":    {},
	"private_key_id": {},
	"clientsecret":   {},
	"client_secret":  {},
	"client_email":   {},
	"apikey":         {},
	"api_key":        {},
	"sessionsecret":  {},
	"email":          {},
	"adminemails":    {},
	"customername":   {},
	"postalcode":     {},
	"city":           {},
	"streetaddress":  {},
}

func normalizeJSONKey(key string) string {
	return strings.ToLower(strings.TrimSpace(key))
}

func hasAtMostTwoDecimals(num json.Number) error {
	raw := num.String()
	if strings.ContainsAny(raw, "eE") {
		return fmt.Errorf("coordinate %q uses scientific notation", raw)
	}
	if dotIdx := strings.IndexByte(raw, '.'); dotIdx >= 0 {
		frac := raw[dotIdx+1:]
		if len(frac) > 2 {
			return fmt.Errorf("coordinate %q has %d decimal places (max 2)", raw, len(frac))
		}
	}
	val, err := num.Float64()
	if err != nil {
		return fmt.Errorf("invalid coordinate number %q: %w", raw, err)
	}
	if math.IsNaN(val) || math.IsInf(val, 0) {
		return fmt.Errorf("invalid non-finite coordinate %q", raw)
	}
	rounded := math.Round(val*100) / 100
	if math.Abs(val-rounded) > 1e-9 {
		return fmt.Errorf("coordinate %v is not rounded to 2 decimal places", val)
	}
	return nil
}

func inspectFixtureJSONValue(value any, jsonPath string) error {
	switch v := value.(type) {
	case map[string]any:
		for k, child := range v {
			childPath := k
			if jsonPath != "" {
				childPath = jsonPath + "." + k
			}
			normKey := normalizeJSONKey(k)
			if _, forbidden := forbiddenFixtureJSONKeys[normKey]; forbidden {
				return fmt.Errorf("%s uses forbidden credential/PII JSON key %q", childPath, k)
			}
			if normKey == "siteid" {
				s, ok := child.(string)
				if !ok || !syntheticSiteIDPattern.MatchString(s) {
					return fmt.Errorf("%s has non-synthetic site ID %v (must match ^site[0-9]+$)", childPath, child)
				}
			}
			if normKey == "latitude" || normKey == "longitude" {
				num, ok := child.(json.Number)
				if !ok {
					return fmt.Errorf("%s must be a JSON number, got %T", childPath, child)
				}
				if err := hasAtMostTwoDecimals(num); err != nil {
					return fmt.Errorf("%s: %w", childPath, err)
				}
			}
			if err := inspectFixtureJSONValue(child, childPath); err != nil {
				return err
			}
		}
	case []any:
		for i, item := range v {
			itemPath := fmt.Sprintf("%s[%d]", jsonPath, i)
			if err := inspectFixtureJSONValue(item, itemPath); err != nil {
				return err
			}
		}
	case string:
		if emailPattern.MatchString(v) {
			return fmt.Errorf("%s contains an email address", jsonPath)
		}
	}
	return nil
}

func validateFixturePrivacy(fileName string, raw []byte, requireWeatherCoords bool) error {
	if emailPattern.Match(raw) {
		return fmt.Errorf("raw fixture %s contains an email-like pattern", fileName)
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()

	var root map[string]any
	if err := dec.Decode(&root); err != nil {
		return fmt.Errorf("unmarshal %s: %w", fileName, err)
	}

	var topSiteID string
	if rawSiteID, ok := root["siteID"]; ok {
		s, ok := rawSiteID.(string)
		if !ok {
			return fmt.Errorf("%s: siteID must be a string", fileName)
		}
		topSiteID = s
	} else if rawSiteId, ok := root["siteId"]; ok {
		s, ok := rawSiteId.(string)
		if !ok {
			return fmt.Errorf("%s: siteId must be a string", fileName)
		}
		topSiteID = s
	} else {
		return fmt.Errorf("%s: missing top-level siteID/siteId", fileName)
	}

	if !syntheticSiteIDPattern.MatchString(topSiteID) {
		return fmt.Errorf("%s: top-level site ID %q does not match ^site[0-9]+$", fileName, topSiteID)
	}

	expectedPrefix := topSiteID + "_"
	if !strings.HasPrefix(fileName, expectedPrefix) || !strings.HasSuffix(fileName, ".json") {
		return fmt.Errorf("%s: filename must start with %q and end with .json", fileName, expectedPrefix)
	}

	if err := inspectFixtureJSONValue(root, ""); err != nil {
		return fmt.Errorf("%s: %w", fileName, err)
	}

	if requireWeatherCoords {
		weatherRaw, ok := root["weatherHistory"]
		if !ok {
			return fmt.Errorf("%s: missing weatherHistory", fileName)
		}
		weatherSlice, ok := weatherRaw.([]any)
		if !ok || len(weatherSlice) == 0 {
			return fmt.Errorf("%s: weatherHistory must be a non-empty array", fileName)
		}
		for i, item := range weatherSlice {
			dayMap, ok := item.(map[string]any)
			if !ok {
				return fmt.Errorf("%s: weatherHistory[%d] must be an object", fileName, i)
			}
			if _, hasLat := dayMap["latitude"]; !hasLat {
				return fmt.Errorf("%s: weatherHistory[%d] missing latitude", fileName, i)
			}
			if _, hasLon := dayMap["longitude"]; !hasLon {
				return fmt.Errorf("%s: weatherHistory[%d] missing longitude", fileName, i)
			}
		}
	}

	return nil
}

func TestValidateFixturePrivacy(t *testing.T) {
	t.Run("ValidEnergyFixture", func(t *testing.T) {
		validJSON := []byte(`{
			"siteID": "site2",
			"period": "mid-summer",
			"timeZone": "America/Chicago",
			"weatherHistory": [
				{"latitude": 41.7, "longitude": -88.07}
			]
		}`)
		require.NoError(t, validateFixturePrivacy("site2_mid-summer.json", validJSON, true))
	})

	t.Run("RejectsNonSyntheticSiteID", func(t *testing.T) {
		rawSiteJSON := []byte(`{
			"siteID": "customer_00",
			"period": "march"
		}`)
		err := validateFixturePrivacy("customer_00_march.json", rawSiteJSON, false)
		assert.ErrorContains(t, err, "does not match ^site[0-9]+$")
	})

	t.Run("RejectsFilenameMismatch", func(t *testing.T) {
		mismatchJSON := []byte(`{
			"siteId": "site3",
			"period": "march"
		}`)
		err := validateFixturePrivacy("site4_march.json", mismatchJSON, false)
		assert.ErrorContains(t, err, `filename must start with "site3_"`)
	})

	t.Run("RejectsEmbeddedEmail", func(t *testing.T) {
		addr := "resident" + "@" + " utility-customer.invalid"
		addr = strings.ReplaceAll(addr, " ", "")
		emailJSON := []byte(fmt.Sprintf(`{
			"siteId": "site1",
			"period": "march",
			"actionHistory": [{"reason": "notified %s"}]
		}`, addr))
		err := validateFixturePrivacy("site1_march.json", emailJSON, false)
		assert.ErrorContains(t, err, "email")
	})

	t.Run("RejectsCredentialKeys", func(t *testing.T) {
		credJSON := []byte(`{
			"siteId": "site1",
			"period": "march",
			"accessToken": "redacted"
		}`)
		err := validateFixturePrivacy("site1_march.json", credJSON, false)
		assert.ErrorContains(t, err, "forbidden credential/PII JSON key")
	})

	t.Run("RejectsUnroundedCoordinates", func(t *testing.T) {
		unroundedJSON := []byte(`{
			"siteID": "site2",
			"period": "mid-summer",
			"weatherHistory": [
				{"latitude": 41.69864, "longitude": -88.07}
			]
		}`)
		err := validateFixturePrivacy("site2_mid-summer.json", unroundedJSON, true)
		assert.ErrorContains(t, err, "decimal places")
	})
}

func TestFixturePrivacy(t *testing.T) {
	planFiles, err := historyFS.ReadDir("testdata/history")
	require.NoError(t, err)
	require.NotEmpty(t, planFiles, "expected plan history fixtures in testdata/history")

	energyFiles, err := energyHistoryFS.ReadDir("testdata/energy_history")
	require.NoError(t, err)
	require.NotEmpty(t, energyFiles, "expected energy history fixtures in testdata/energy_history")

	checked := make(map[string]struct{}, len(planFiles)+len(energyFiles))

	t.Run("PlanHistoryFixtures", func(t *testing.T) {
		for _, entry := range planFiles {
			fileName := entry.Name()
			relPath := "testdata/history/" + fileName
			checked[filepath.ToSlash(relPath)] = struct{}{}

			t.Run(fileName, func(t *testing.T) {
				raw, err := historyFS.ReadFile(relPath)
				require.NoError(t, err)
				require.NoError(t, validateFixturePrivacy(fileName, raw, false))
			})
		}
	})

	t.Run("EnergyHistoryFixtures", func(t *testing.T) {
		for _, entry := range energyFiles {
			fileName := entry.Name()
			relPath := "testdata/energy_history/" + fileName
			checked[filepath.ToSlash(relPath)] = struct{}{}

			t.Run(fileName, func(t *testing.T) {
				raw, err := energyHistoryFS.ReadFile(relPath)
				require.NoError(t, err)
				require.NoError(t, validateFixturePrivacy(fileName, raw, true))
			})
		}
	})

	t.Run("AllDiskFixturesCovered", func(t *testing.T) {
		var diskFixtures []string
		err := filepath.WalkDir("testdata", func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if !d.IsDir() && strings.HasSuffix(d.Name(), ".json") {
				diskFixtures = append(diskFixtures, filepath.ToSlash(path))
			}
			return nil
		})
		require.NoError(t, err)
		require.NotEmpty(t, diskFixtures)

		for _, diskPath := range diskFixtures {
			if _, ok := checked[diskPath]; !ok {
				raw, readErr := os.ReadFile(diskPath)
				require.NoError(t, readErr)
				require.NoError(t, validateFixturePrivacy(filepath.Base(diskPath), raw, false),
					"unembedded fixture %s failed privacy validation", diskPath)
			}
		}
	})
}
