## Roadmap
v0.1
* [x] Controlled scheduling of deployments according to the CRD spec
* [x] Allow updates of deployments (store generation of the deployment in status)
* [x] Propagate partition ID to the managed deployment
* [x] Adding or removing deployments based on CRD spec

v0.2
* [x] Update GOMAXPROCS based on number of CPUs
* [x] Initial resource allocation based current Kafka metrics + static multipliers
* [ ] Autoscaling based on Production/Consumption/Offset
* [ ] Expose metrics about own health and behaviour
* [ ] use `scale` as a way to pause/resume the consumer
* [ ] add ignore list of container names (do not do anything with sidecars)
* [ ] Scale down after period X of normal operations

v0.3
* [ ] Expose metrics about own health and behaviour
* [ ] Recreate deployments from scratch, if any of the immutable fields were changed in the deploymentSpec
      Now, it requires manual deleting of all deployments.


v0.4
* [ ] 

-------
Unsorted
* [ ] [Feature] consider storing MetricsMap from the last query in consumer object status
* [ ] [Feature] build simple service to produce pseudo-data to local kafka/prometheus
* [ ] [Feature] post behaviour updates to Kubernetes events
* [ ] [Feature] scale up without restart [blocked](https://github.com/kubernetes/kubernetes/issues/5774)
* [ ] [Feature] call webhooks on scaling events
* [ ] [Feature] Vertical autoscalling of balanced workloads (single deployment)
* [ ] [Feature] Fully dynamic resource allocations based on historic data
 