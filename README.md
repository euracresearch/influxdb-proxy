# InfluxDB reverse proxy

The proxy checks incoming InfluxQL SELECT queries and will forward them to the given Influx database if the data source (measurement), extracted from the query, is in the given allowed list of measurements.
All other queries will return an error to the client.

# License

The library is distributed under the BSD-style license found in [LICENSE](./LICENSE) file.
