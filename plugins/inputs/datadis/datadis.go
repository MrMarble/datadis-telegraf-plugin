package datadis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/inputs"
	"golang.org/x/sync/errgroup"
)

const URL = "https://datadis.es"

type MarshableTime struct {
	time.Time
}

func (m *MarshableTime) UnmarshalJSON(b []byte) (err error) {
	s := string(b)
	s = s[1 : len(s)-1]
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return err
	}
	m.Time = t
	return nil
}

type Supply struct {
	Address         string `json:"address"`
	Cups            string `json:"cups"`
	PostalCode      string `json:"postalCode"`
	Province        string `json:"province"`
	Municipality    string `json:"municipality"`
	Distributor     string `json:"distributor"`
	ValidDateFrom   string `json:"validDateFrom"`
	ValidDateTo     string `json:"validDateTo"`
	PointType       uint8  `json:"pointType"`
	DistributorCode string `json:"distributorCode"`
}

type Consumption struct {
	cups         string
	date         string
	time         string
	kWh          float64
	obtainMethod string
}

func (c *Consumption) timestamp() (*time.Time, error) {
	t, err := time.Parse("2006-01-02 15:04", fmt.Sprintf("%v %v", c.date, c.time))
	if err != nil {
		return nil, err
	}
	return &t, err
}

type measurementType int

const (
	HOURLY measurementType = iota
	QUARTER_HOURLY
)

type Datadis struct {
	HTTPTimeout     config.Duration `toml:"http_timeout"`
	MeasurementType measurementType `toml:"measurement_type"`
	httpClient      *http.Client
	username        string
	password        string
	token           string
	supplies        []Supply
}

func (p *Datadis) createHTTPClient() *http.Client {
	client := http.Client{Timeout: time.Duration(p.HTTPTimeout)}
	return &client
}

func (d *Datadis) refreshToken() error {
	authUrl, _ := url.Parse(URL)

	authUrl.Path = "/nikola-auth/tokens/login"

	q := authUrl.Query()
	q.Set("username", d.username)
	q.Set("password", d.password)

	authUrl.RawQuery = q.Encode()

	resp, err := d.httpClient.Post(authUrl.String(), "", nil)
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
		return errors.New(fmt.Sprintf("error fetching token. Response status: %v - %v", resp.StatusCode, resp.Status))
	}

	return nil
}

func (d *Datadis) getSupplies() error {
	supplyUrl, _ := url.Parse(URL)
	supplyUrl.Path = "/api-private/api/get-supplies"

	resp, err := d.httpClient.Get(supplyUrl.String())
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var data []Supply

	err = json.NewDecoder(resp.Body).Decode(&data)
	if err != nil {
		return err
	}
	d.supplies = data
	return nil
}

func (d *Datadis) fetchAllConsumptions(ctx context.Context) ([]Consumption, error) {
	errs, ctx := errgroup.WithContext(ctx)

	var consumptions []Consumption
	for _, supply := range d.supplies {
		errs.Go(func() error {
			consumptionUrl, _ := url.Parse(URL)
			consumptionUrl.Path = "/api-private/api/get-consumption-data"

			q := consumptionUrl.Query()
			q.Set("cups", supply.Cups)
			q.Set("distributorCode", supply.DistributorCode)
			q.Set("measurementType", string(d.MeasurementType))
			q.Set("pointType", string(supply.PointType))

			resp, err := d.httpClient.Get(consumptionUrl.String())
			if err != nil {
				return err
			}
			defer resp.Body.Close()

			var data []Consumption
			err = json.NewDecoder(resp.Body).Decode(&data)
			if err != nil {
				return err
			}
			consumptions = append(consumptions, data...)
			return nil
		})
	}

	return consumptions, errs.Wait()
}

func (d *Datadis) Description() string {
	return "Gather information about your energy consumption from datadis."
}

func (d *Datadis) SampleConfig() string {
	return `
    ## Datadis username. Required
    #username=""
    ## Datadis password. Required
    #password=""

    ## HTTP Request timeout.
    http_timeout="30s"

    ## Measurement type.
    ## 0 (Zero) => hourly consumption
    ## 1 (One) => quarter hourly consumption
    measurement_type = 1
`
}

// Init is for setup, and validating config.
func (d *Datadis) Init() error {
	if d.httpClient == nil {
		d.httpClient = d.createHTTPClient()
	}

	err := d.refreshToken()
	return err
}

func (d *Datadis) Gather(acc telegraf.Accumulator) error {
	data, err := d.fetchAllConsumptions(context.Background())
	if err != nil {
		return err
	}

	for _, consumption := range data {
		fields := map[string]interface{}{"kwh": consumption.kWh}
		tags := map[string]string{"cups": consumption.cups, "obtain_method": consumption.obtainMethod}

		timestamp, err := consumption.timestamp()
		if err != nil {
			return err
		}
		acc.AddFields("Datadis", fields, tags, *timestamp)
	}
	return nil
}

func init() {
	inputs.Add("Datadis", func() telegraf.Input { return &Datadis{} })
}
