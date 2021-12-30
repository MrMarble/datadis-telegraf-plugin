package datadis

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/influxdata/telegraf/config"
)

func TestFetchConsumption(t *testing.T) {
	endDate := time.Now().Format("2006/01/02")
	startDate := time.Now().Add(-24 * time.Hour).Format("2006/01/02")

	ts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("startDate") != startDate {
			t.Fatalf("expected: %q, got: %q", startDate, query.Get("startDate"))
		}
		if query.Get("endDate") != endDate {
			t.Fatalf("expected: %q, got: %q", endDate, query.Get("endDate"))
		}

		fmt.Fprint(rw, `[ {
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
		  } ]`)
	}))
	defer ts.Close()

	t.Run("Should use time range", func(t *testing.T) {
		d := Datadis{
			url:        ts.URL,
			httpClient: ts.Client(),
			StartDate:  startDate,
			EndDate:    endDate,
		}

		_, err := fetchConsumption(d, Supply{})
		if err != nil {
			t.Fatal(err)
		}

	})
	t.Run("Should calculate time range", func(t *testing.T) {
		d := Datadis{
			url:          ts.URL,
			httpClient:   ts.Client(),
			DateDuration: config.Duration(24 * time.Hour),
		}

		_, err := fetchConsumption(d, Supply{})
		if err != nil {
			t.Fatal(err)
		}
	})
	t.Run("Should parse response", func(t *testing.T) {
		d := Datadis{
			url:        ts.URL,
			httpClient: ts.Client(),
			StartDate:  startDate,
			EndDate:    endDate,
		}

		got, err := fetchConsumption(d, Supply{})
		if err != nil {
			t.Fatal(err)
		}

		if len(got) != 2 {
			t.Fatalf("expected: %d, got: %d", 2, len(got))
		}

		timestamp, err := got[1].timestamp()
		if err != nil {
			t.Fatal(err)
		}

		if timestamp.Unix() != 1640649600 {
			t.Fatalf("expected: %d, got: %d", 1640649600, timestamp.Unix())
		}

		if got[0].KWh != 0.121 {
			t.Fatalf("expected: %f, got: %f", 0.121, got[0].KWh)
		}
	})
}
