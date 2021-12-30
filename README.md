# Datadis Telegraf Plugin

[![golangci-lint](https://github.com/MrMarble/datadis-telegraf-plugin/actions/workflows/lint.yml/badge.svg)](https://github.com/MrMarble/datadis-telegraf-plugin/actions/workflows/lint.yml)

Gather Spanish energy consumption from https://datadis.es.

## Configuration

```toml
[[inputs.Datadis]]
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

```

## Metrics

- Datadis
    - tags:
        - cups (string)
        - obtain_method (string)
    - fields:
        - kwh (float64)

## Example Output

```
Datadis,cups=ES0099999999999999AAAA,obtain_method=Real kwh=0.368 1640782800000000000
Datadis,cups=ES0099999999999999AAAA,obtain_method=Real kwh=0.745 1640786400000000000
```
