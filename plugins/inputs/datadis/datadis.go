package datadis

import (
	"context"
	"encoding/json"
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
	QuarterHourly
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

func (d *Datadis) createHTTPClient() *http.Client {
	client := http.Client{Timeout: time.Duration(d.HTTPTimeout)}
	return &client
}

func (d *Datadis) refreshToken() error {
	authURL, _ := url.Parse(URL)

	authURL.Path = "/nikola-auth/tokens/login"

	q := authURL.Query()
	q.Set("username", d.username)
	q.Set("password", d.password)

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

	return nil
}

func (d *Datadis) getSupplies() error {
	supplyURL, _ := url.Parse(URL)
	supplyURL.Path = "/api-private/api/get-supplies"

	resp, err := d.httpClient.Get(supplyURL.String())
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
	errs, _ := errgroup.WithContext(ctx)

	var consumptions []Consumption
	for _, supply := range d.supplies {
		errs.Go(func(_supply Supply) func() error {
			return func() error {
				consumptionURL, _ := url.Parse(URL)
				consumptionURL.Path = "/api-private/api/get-consumption-data"

				q := consumptionURL.Query()
				q.Set("cups", _supply.Cups)
				q.Set("distributorCode", _supply.DistributorCode)
				q.Set("measurementType", fmt.Sprint(d.MeasurementType))
				q.Set("pointType", fmt.Sprint(_supply.PointType))

				resp, err := d.httpClient.Get(consumptionURL.String())
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
			}
		}(supply))
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
	if err != nil {
		return err
	}
	err = d.getSupplies()
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
