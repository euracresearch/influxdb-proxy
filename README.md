# InfluxDB reverse proxy

The proxy check incoming InfluxQL SELECT queries and will proxy them to the given Influx database if the data source (measurement), extracted form the FROM field of the query is allowed.

All other queries will return an error to the client.

# License

The library is distributed under the BSD-style license found in [LICENSE](./LICENSE) file.
