package datadis

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/influxdata/telegraf/config"
)

func NewServer() *httptest.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/api-private/api/get-consumption-data", func(rw http.ResponseWriter, r *http.Request) {
		rw.Write([]byte(`[ {
			"cups" : "1234",
			"date" : "2021/12/28",
			"time" : "01:00",
			"consumptionKWh" : 0.121,
			"obtainMethod" : "Real"
		  }, {
			"cups" : "1234",
			"date" : "2021/12/28",
			"time" : "24:00",
			"consumptionKWh" : 0.117,
			"obtainMethod" : "Real"
		  } ]`))
	})

	ts := httptest.NewServer(mux)

	return ts
}

func TestFetchAll(t *testing.T) {
	ts := NewServer()
	defer ts.Close()

	d := Datadis{
		Username:        "u",
		Password:        "p",
		token:           "t",
		MeasurementType: HOURLY,
		DateDuration:    config.Duration(24 * time.Hour),
		url:             ts.URL,
		Supplies:        []Supply{{Cups: "C", DistributorCode: "2", PointType: 0}},
		httpClient:      ts.Client(),
	}

	data, err := fetchConsumption(d, d.Supplies[0])
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if len(data) != 2 {
		t.Fatalf("wanted: 2 got %d", len(data))
	}

	_, err = data[1].timestamp()
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
}
