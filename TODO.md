## Bugs

* [x] fix case when prometheus is not available
* [x] reducing number of replicas should delete old deployments 

## Features
* [ ] resource requirements based on topic production distribution 
* [ ] store generation of deployment spec to allow updates 
* [ ] build simple service to produce pseudo-data to local kafka/prometheus
* [ ] expose metrics about own health and behaviour
* [ ] update GOMAXPROCS based on number of CPUs
* [ ] post behaviour updates to Kubernetes events


## Future
* [ ] dynamic scaling up/down based on metrics
* [ ] scale up without restart 
* [ ] call webhooks on scaling events 
