## Roadmap
v0.1
* [x] Controlled scheduling of deployments according to the CRD spec
* [x] Allow updates of deployments (store generation of the deployment in status)
* [x] Propagate partition ID to the managed deployment
* [x] Adding or removing deployments based on CRD spec

v0.2
* [ ] Recreate deployments from scratch, if any of the immutable fields were changed in the deploymentSpec
      Now, it requires manual deleting of all deployments.
* [x] Update GOMAXPROCS based on number of CPUs
* [ ] Initial resource allocation based current Kafka metrics + static multipliers
* [ ] Autoscaling based on Production/Consumption/Offset
* [ ] Expose metrics about own health and behaviour
* [ ] use `scale` as a way to pause/resume the consumer

v0.3
* [ ] Fully dynamic resource allocations based on historic data

v0.4
* [ ] 

-------
Unsorted
* [ ] [Feature] build simple service to produce pseudo-data to local kafka/prometheus
* [ ] [Feature] post behaviour updates to Kubernetes events
* [ ] [Feature] scale up without restart [blocked](https://github.com/kubernetes/kubernetes/issues/5774)
* [ ] [Feature] call webhooks on scaling events
* [ ] [Feature] Vertical autoscalling of balanced workloads (single deployment) 