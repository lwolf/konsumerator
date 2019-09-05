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
* [x] Query multiple prometheus for metrics
* [x] Write readme

v0.4 - observability
* [ ] Cleanup logging
* [ ] Validate/Fix RBAC permissions
* [x] Post behaviour updates to Kubernetes events
* [ ] Expose metrics about own health and behaviour
* [ ] Grafana dashboard
* [ ] Scale down only when no lag present
* [ ] Scale down after X periods of no lag

v0.5
* [ ] Try replacing Deployment with ReplicaSet for simplicity 
* [ ] Recreate deployments from scratch, if any of the immutable fields were changed in the deploymentSpec
      Now, it requires manual deleting of all deployments.
* [ ] Add ignore list of container names (do not do anything with sidecars)
      Current workaround - set resource policy for those containers with min=max resources

-------
Unsorted
* [ ] [TEST] Add more integration tests 
* [ ] [TEST] add test to verify that env variables are always set 
* [ ] [BUG] update of the autoscaler spec (ratePerCore, ramPerCore) should ? trigger reconciliation
* [ ] [BUG] fix the logic for calculation hash of deploymentSpec (should always be positive)
* [ ] [Feature] build simple service to produce pseudo-data to local kafka/prometheus

* [ ] [Feature] call webhooks on scaling events
* [ ] [Feature] Vertical autoscalling of balanced workloads (single deployment)
* [ ] [Feature] Fully dynamic resource allocations based on historic data
* [ ] [Feature] ? Add totalMaxAllowed which will limit total number of cores available for the consumer
* [ ] [Feature] ? consider adding support for VPA/HPA 
* [ ] [Feature] ? Use `scale` as a way to pause/resume the consumer
* [ ] [Feature] ? Tool for operations `consumerctl stop/start consumer`
* [ ] [Feature] ? Ability to set additional deployment-level annotations/labels ?
* [ ] [Feature] scale up without restart [blocked](https://github.com/kubernetes/kubernetes/issues/5774)
