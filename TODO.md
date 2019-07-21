## Roadmap
v0.1
* [x] Controlled scheduling of deployments according to the CRD spec
* [x] Allow updates of deployments (store generation of the deployment in status)
* [x] Propagate partition ID to the managed deployment
* [x] Adding or removing deployments based on CRD spec

v0.2
* [x] Update GOMAXPROCS based on number of CPUs
* [x] Initial resource allocation based on current Kafka metrics + static multipliers
* [x] Autoscaling based on Production/Consumption/Offset
* [x] Store MetricsMap from the last query in consumer object status
* [x] Rename static predictor to `naive`
* [x] Load metricsProvider from the status

v0.3
* [x] Setup travis-ci
* [ ] Use `scale` as a way to pause/resume the consumer
* [ ] Query multiple prometheus for metrics
* [ ] Write readme

v0.4 - observability
* [ ] Cleanup logging
* [ ] Post behaviour updates to Kubernetes events
* [ ] Expose metrics about own health and behaviour
* [ ] Grafana dasboard

v0.5
* [ ] Recreate deployments from scratch, if any of the immutable fields were changed in the deploymentSpec
      Now, it requires manual deleting of all deployments.
* [ ] Add ignore list of container names (do not do anything with sidecars)
      Current workaround - set resource policy for those containers with min=max resources
* [ ] Scale down after period X of normal operations 

-------
Unsorted
* [ ] Add more integration tests 
* [ ] [Feature] build simple service to produce pseudo-data to local kafka/prometheus
* [ ] [Feature] scale up without restart [blocked](https://github.com/kubernetes/kubernetes/issues/5774)
* [ ] [Feature] call webhooks on scaling events

* [ ] [Feature] Vertical autoscalling of balanced workloads (single deployment)
* [ ] [Feature] Fully dynamic resource allocations based on historic data
* [ ] [Feature] consider adding support for VPA/HPA 
 