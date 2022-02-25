# InfluxDB reverse proxy [![Test Status](https://github.com/euracresearch/influxdb-proxy/workflows/Test/badge.svg)](https://github.com/euracresearch/influxdb-proxy/actions) [![Go Report Card](https://goreportcard.com/badge/euracresearch/influxdb-proxy)](https://goreportcard.com/report/github.com/euracresearch/influxdb-proxy)

The proxy checks incoming InfluxQL SELECT queries and will forward them to the given Influx database if the data source (measurement), extracted from the query, is in the given allowed list of measurements.
All other queries will return an error to the client.

# License

The application is distributed under the BSD-style license found in [LICENSE](./LICENSE) file.
