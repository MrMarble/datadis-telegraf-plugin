package datadis

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/plugins/inputs"
	"golang.org/x/sync/errgroup"
)

const (
	URL                    = "https://datadis.es"
	HOURLY measurementType = iota
	QuarterHourly
)

type (
	// Datadis contains the configuration for the pluguin.
	Datadis struct {
		HTTPTimeout     config.Duration `toml:"http_timeout"`
		MeasurementType measurementType `toml:"measurement_type"`
		Username        string          `toml:"username"`
		Password        string          `toml:"password"`
		Supplies        []Supply        `toml:"supplies"`
		StartDate       string          `toml:"start_date"`
		EndDate         string          `toml:"end_date"`
		DateDuration    config.Duration `toml:"date_duration"`
		url             string
		token           string
		httpClient      *http.Client

		Log telegraf.Logger `toml:"-"`
	}

	Supply struct {
		Address         string `json:"address"`
		Cups            string `json:"cups" toml:"cups"`
		PostalCode      string `json:"postalCode"`
		Province        string `json:"province"`
		Municipality    string `json:"municipality"`
		Distributor     string `json:"distributor"`
		ValidDateFrom   string `json:"validDateFrom"`
		ValidDateTo     string `json:"validDateTo"`
		PointType       uint8  `json:"pointType" toml:"point_type"`
		DistributorCode string `json:"distributorCode" toml:"distributor_code"`
	}
	Consumption struct {
		Cups         string
		Date         string
		Time         string
		KWh          float64 `json:"consumptionKWh"`
		ObtainMethod string
	}

	measurementType int
)

func (c *Consumption) timestamp() (*time.Time, error) {
	t, err := time.Parse("2006/01/02 15:04", fmt.Sprintf("%v %v", c.Date, strings.Replace(c.Time, "24:", "00:", 1)))
	if err != nil {
		return nil, err
	}
	return &t, err
}

// Description returns a one-sentence description on the Datadis input plugin.
func (d *Datadis) Description() string {
	return "Gather information about your energy consumption from datadis."
}

// SampleConfig returns the default configuration of the Datadis input plugin.
func (d *Datadis) SampleConfig() string {
	return `
    ## Datadis username. Required.
    username = ""
    ## Datadis password. Required.
    password = ""

    ## HTTP Request timeout.
    http_timeout = "1m"

    ## Measurement type.
    ##  0 (Zero) => hourly consumption.
    ##  1 (One) => quarter hourly consumption.
    measurement_type = 0

    ## Date range.
    ##  Use for static dates
    ##  If omitted will use date_duration
    ##  Format => 2021/01/26
    start_date = ""
    end_date = ""
    ## Duration.
    ##  Use for dynamic dates
    date_duration = "168h"

    ## Supplies
    ## Skip fetching supplies
    ## [[inputs.Datadis.supplies]]
    ##     cups = ""
    ##     point_type = 5
    ##     distributor_code = "2"
`
}

// Gather takes in an accumulator and adds the metrics that the Input
// gathers. This is called every "interval".
func (d *Datadis) Gather(acc telegraf.Accumulator) error {

	err := d.initializeClient()
	if err != nil {
		return err
	}

	wg := sync.WaitGroup{}
	rLock := sync.Mutex{}

	metrics := []Consumption{}

	wg.Add(1)
	go func() {
		defer wg.Done()

		result, err := d.fetchAllConsumptions()
		if err != nil {
			acc.AddError(err)
			return
		}

		rLock.Lock()
		metrics = result
		rLock.Unlock()
	}()
	wg.Wait()

	return d.aggregateMetrcs(acc, metrics)
}

func (d *Datadis) initializeClient() error {
	if d.httpClient == nil {
		client := http.Client{Timeout: time.Duration(d.HTTPTimeout)}
		d.httpClient = &client
	}

	err := d.refreshToken()
	if err != nil {
		return err
	}

	if d.Supplies == nil {
		err := d.getSupplies()
		if err != nil {
			return err
		}
	}
	return nil
}

func (d *Datadis) refreshToken() error {
	authURL, _ := url.Parse(URL)

	authURL.Path = "/nikola-auth/tokens/login"

	q := authURL.Query()
	q.Set("username", d.Username)
	q.Set("password", d.Password)

	authURL.RawQuery = q.Encode()
	resp, err := d.httpClient.Post(authURL.String(), "", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		token, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		d.token = string(token)
	} else {
		return fmt.Errorf("error fetching token. Response status: %v - %v", resp.StatusCode, resp.Status)
	}

	d.Log.Debug("Token refreshed")
	return nil
}

func (d *Datadis) getSupplies() error {
	d.Log.Debug("fetching supplies")
	supplyURL, _ := url.Parse(URL)
	supplyURL.Path = "/api-private/api/get-supplies"

	req, err := http.NewRequest("GET", supplyURL.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %v", d.token))
	resp, err := d.httpClient.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		var data []Supply
		err = json.NewDecoder(resp.Body).Decode(&data)
		if err != nil {
			return err
		}
		d.Supplies = data
	} else {
		return fmt.Errorf("error fetching supplies. Response status: %v - %v", resp.StatusCode, resp.Status)
	}
	return nil
}

func fetchConsumption(d Datadis, supply Supply) ([]Consumption, error) {
	consumptionURL, _ := url.Parse(d.url)
	consumptionURL.Path = "/api-private/api/get-consumption-data"

	params := url.Values{
		"cups":            {supply.Cups},
		"distributorCode": {supply.DistributorCode},
		"measurementType": {fmt.Sprint(d.MeasurementType)},
		"pointType":       {fmt.Sprint(supply.PointType)},
	}

	if d.StartDate != "" && d.EndDate != "" {
		params.Set("startDate", d.StartDate)
		params.Set("endDate", d.EndDate)
	} else {
		params.Set("startDate", time.Now().Add(time.Duration(-d.DateDuration)).Format("2006/01/02"))
		params.Set("endDate", time.Now().Format("2006/01/02"))
	}

	consumptionURL.RawQuery = params.Encode()

	req, err := http.NewRequest("GET", consumptionURL.String(), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Accept", "application/json")
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %v", d.token))

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	var data []Consumption
	if resp.StatusCode == 200 {
		err = json.NewDecoder(resp.Body).Decode(&data)
		if err != nil {
			return nil, err
		}
	} else {
		return nil, fmt.Errorf("error fetching consumption. Response status: %v - %v", resp.StatusCode, resp.Status)
	}

	return data, nil
}

func (d *Datadis) fetchAllConsumptions() ([]Consumption, error) {
	errs, _ := errgroup.WithContext(context.Background())

	var consumptions []Consumption
	for _, supply := range d.Supplies {
		supply := supply
		errs.Go(func() error {

			data, err := fetchConsumption(*d, supply)
			consumptions = append(consumptions, data...)
			return err
		})
	}

	errors := errs.Wait()
	return consumptions, errors
}

func (d *Datadis) aggregateMetrcs(acc telegraf.Accumulator, metrics []Consumption) error {
	var (
		grouper = metric.NewSeriesGrouper()
		er      error
	)

	for _, consumption := range metrics {
		tags := map[string]string{"cups": consumption.Cups, "obtain_method": consumption.ObtainMethod}

		timestamp, err := consumption.timestamp()
		if err != nil {
			acc.AddError(err)
			er = err
		}
		err = grouper.Add("Datadis", tags, *timestamp, "kwh", consumption.KWh)
		if err != nil {
			acc.AddError(err)
			er = err
		}
	}

	for _, metric := range grouper.Metrics() {
		acc.AddMetric(metric)
	}

	return er
}

// Init is for setup, and validating config.
func (d *Datadis) Init() error {
	d.Log.Debugf("Datadis loaded %#v", d)
	return nil
}

func init() {
	inputs.Add("Datadis", func() telegraf.Input { return &Datadis{url: URL} })
}
