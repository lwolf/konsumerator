## Roadmap
v0.1
* [x] Controlled scheduling of deployments according to the CRD spec
* [ ] Allow updates of deployments (store generation of the deployment in status)
* [x] Adding or removing deployments based on CRD spec
* [ ] Expose metrics about own health and behaviour
* [ ] Propagate generated ID to the managed deployment

v0.2
* [ ] Initial resource allocation based on Kafka production rate (24h window)

v0.3
* [ ] Autoscaling based on Production/Consumption/Offset
* [ ] Update GOMAXPROCS based on number of CPUs

v0.4
* [ ] 

-------
Unsorted
* [ ] [Feature] build simple service to produce pseudo-data to local kafka/prometheus
* [ ] [Feature] post behaviour updates to Kubernetes events
* [ ] [Feature] scale up without restart [blocked](https://github.com/kubernetes/kubernetes/issues/5774)
* [ ] [Feature] call webhooks on scaling events
