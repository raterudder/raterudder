package utility

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/raterudder/raterudder/pkg/types"
)

func createZip(t *testing.T, filename, content string) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)
	f, err := w.Create(filename)
	require.NoError(t, err)
	_, err = f.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return buf.Bytes()
}

func TestERCOT_Mocked(t *testing.T) {
	now := time.Now().In(ctLocation)
	dateStr := now.Format("20060102")

	damCSV := `DeliveryDate,HourEnding,SettlementPoint,SettlementPointPrice,DSTFlag
03/21/2026,1,LZ_HOUSTON,25.00,N
03/21/2026,2,LZ_HOUSTON,20.00,N
`
	damZip := createZip(t, "dam.csv", damCSV)

	rtmCSV := `DeliveryDate,DeliveryHour,DeliveryInterval,SettlementPointName,SettlementPointType,SettlementPointPrice,DSTFlag
03/21/2026,1,1,LZ_HOUSTON,LZ,10.00,N
03/21/2026,1,2,LZ_HOUSTON,LZ,15.00,N
03/21/2026,1,3,LZ_HOUSTON,LZ,20.00,N
03/21/2026,1,4,LZ_HOUSTON,LZ,25.00,N
03/21/2026,2,1,LZ_HOUSTON,LZ,30.00,N
`
	rtmZip := createZip(t, "rtm.csv", rtmCSV)

	scedCSV := `SCEDTimestamp,RepeatedHourFlag,SettlementPoint,SettlementPointPrice
03/21/2026 12:15:30,N,LZ_HOUSTON,45.00
`
	scedZip := createZip(t, "sced.csv", scedCSV)

	mux := http.NewServeMux()
	mux.HandleFunc("/misapp/servlets/IceDocListJsonWS", func(w http.ResponseWriter, r *http.Request) {
		reportType := r.URL.Query().Get("reportTypeId")
		var docList ercotDocumentListRes

		doc := struct {
			DocID        string `json:"DocID"`
			Extension    string `json:"Extension"`
			FriendlyName string `json:"FriendlyName"`
		}{
			Extension: "zip",
		}

		if reportType == ercotReportTypeDAM {
			doc.DocID = "doc_dam"
			doc.FriendlyName = "DAMSPNP_" + dateStr + "_csv"
		} else if reportType == ercotReportTypeRTM {
			doc.DocID = "doc_rtm"
			doc.FriendlyName = "SPPHLZNP_" + dateStr + "_1200_csv"
		} else if reportType == ercotReportTypeSCED {
			doc.DocID = "doc_sced"
			doc.FriendlyName = "LMPSROSNODENP_" + dateStr + "_122016_csv"
		} else {
			http.Error(w, "unknown report type", http.StatusBadRequest)
			return
		}

		docList.ListDocsByRptTypeRes.DocumentList = append(docList.ListDocsByRptTypeRes.DocumentList, struct {
			Document struct {
				DocID        string `json:"DocID"`
				Extension    string `json:"Extension"`
				FriendlyName string `json:"FriendlyName"`
			} `json:"Document"`
		}{Document: doc})

		json.NewEncoder(w).Encode(docList)
	})

	mux.HandleFunc("/misdownload/servlets/mirDownload", func(w http.ResponseWriter, r *http.Request) {
		docID := r.URL.Query().Get("doclookupId")
		if docID == "doc_dam" {
			w.Write(damZip)
		} else if docID == "doc_rtm" {
			w.Write(rtmZip)
		} else if docID == "doc_sced" {
			w.Write(scedZip)
		} else {
			http.Error(w, "not found", http.StatusNotFound)
		}
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	ercot := NewBaseERCOT()
	ercot.listAPIURL = srv.URL + "/misapp/servlets/IceDocListJsonWS"
	ercot.downloadURL = srv.URL + "/misdownload/servlets/mirDownload"

	ctx := context.Background()

	t.Run("GetFuturePrices_DAM", func(t *testing.T) {
		prices, err := ercot.GetFuturePrices(ctx, "LZ_HOUSTON")
		require.NoError(t, err)
		assert.Len(t, prices, 2)

		assert.Equal(t, 25.00/1000.0, prices[0].DollarsPerKWH)
		assert.Equal(t, 20.00/1000.0, prices[1].DollarsPerKWH)
	})

	t.Run("GetConfirmedPrices_RTM", func(t *testing.T) {
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, ctLocation)
		prices, err := ercot.GetConfirmedPrices(ctx, "LZ_HOUSTON", start, start.Add(24*time.Hour))
		require.NoError(t, err)

		assert.Len(t, prices, 2)
		assert.Equal(t, 17.50/1000.0, prices[0].DollarsPerKWH)
		assert.Equal(t, 30.00/1000.0, prices[1].DollarsPerKWH)
	})

	t.Run("GetCurrentPrice_FallbackToSCED", func(t *testing.T) {
		ercot.mu.Lock()
		ercot.rtmCache = make(map[string][]types.Price)
		ercot.rtmCacheTime = time.Time{}
		ercot.mu.Unlock()

		mux2 := http.NewServeMux()
		mux2.HandleFunc("/misapp/servlets/IceDocListJsonWS", func(w http.ResponseWriter, r *http.Request) {
			reportType := r.URL.Query().Get("reportTypeId")
			if reportType == ercotReportTypeRTM {
				var emptyList ercotDocumentListRes
				json.NewEncoder(w).Encode(emptyList)
				return
			}
			mux.ServeHTTP(w, r)
		})
		mux2.HandleFunc("/misdownload/servlets/mirDownload", mux.ServeHTTP)

		srv2 := httptest.NewServer(mux2)
		defer srv2.Close()

		ercotFallback := NewBaseERCOT()
		ercotFallback.listAPIURL = srv2.URL + "/misapp/servlets/IceDocListJsonWS"
		ercotFallback.downloadURL = srv2.URL + "/misdownload/servlets/mirDownload"

		currPrice, err := ercotFallback.GetCurrentPrice(ctx, "LZ_HOUSTON")
		require.NoError(t, err)
		assert.Equal(t, 45.00/1000.0, currPrice.DollarsPerKWH)
	})

	t.Run("Cross_Zone_Caching", func(t *testing.T) {
		damCSV2 := `DeliveryDate,HourEnding,SettlementPoint,SettlementPointPrice,DSTFlag
03/21/2026,1,LZ_HOUSTON,25.00,N
03/21/2026,2,LZ_HOUSTON,20.00,N
03/21/2026,1,LZ_NORTH,35.00,N
03/21/2026,2,LZ_NORTH,40.00,N
`
		damZip2 := createZip(t, "dam.csv", damCSV2)

		// Force ercot to fetch and cache again by resetting time
		ercot.mu.Lock()
		ercot.damCacheTime = time.Time{}
		ercot.mu.Unlock()

		mux.HandleFunc("/misdownload/servlets/mirDownload2", func(w http.ResponseWriter, r *http.Request) {
			w.Write(damZip2)
		})

		ercot.downloadURL = srv.URL + "/misdownload/servlets/mirDownload2"

		prices, err := ercot.GetFuturePrices(ctx, "LZ_HOUSTON")
		require.NoError(t, err)
		assert.Len(t, prices, 2)

		// This should hit the cache we just built
		prices2, err := ercot.GetFuturePrices(ctx, "LZ_NORTH")
		require.NoError(t, err)
		assert.Len(t, prices2, 2)
		assert.Equal(t, 35.00/1000.0, prices2[0].DollarsPerKWH)
		assert.Equal(t, 40.00/1000.0, prices2[1].DollarsPerKWH)
	})
}

func TestERCOT_Integration(t *testing.T) {
	ercot := NewBaseERCOT()
	ctx := context.Background()

	t.Run("GetFuturePrices", func(t *testing.T) {
		prices, err := ercot.GetFuturePrices(ctx, "LZ_HOUSTON")
		require.NoError(t, err)
		assert.True(t, len(prices) >= 23, "should have at least 23 prices for day ahead")
		for _, p := range prices {
			assert.NotZero(t, p.TSStart)
			assert.NotZero(t, p.TSEnd)
			assert.True(t, p.TSEnd.After(p.TSStart))
		}
	})

	t.Run("GetCurrentPrice", func(t *testing.T) {
		currPrice, err := ercot.GetCurrentPrice(ctx, "LZ_HOUSTON")
		require.NoError(t, err)
		assert.NotZero(t, currPrice.TSStart)
	})
}
