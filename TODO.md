## Roadmap
v0.1
* [x] Controlled scheduling of deployments according to the CRD spec
* [x] Allow updates of deployments (store generation of the deployment in status)
* [x] Propagate partition ID to the managed deployment
* [x] Adding or removing deployments based on CRD spec

v0.2
* [x] Update GOMAXPROCS based on number of CPUs
* [x] Initial resource allocation based on current Kafka metrics + static multipliers
* [x] Auto-scaling based on Production/Consumption/Offset
* [x] Store MetricsMap from the last query in consumer object status
* [x] Rename static predictor to `naive`
* [x] Load metricsProvider from the status

v0.3
* [x] Setup travis-ci
* [x] Query multiple prometheus for metrics
* [x] Write readme

v0.4
* [x] Validate/Fix RBAC permissions
* [x] build simple service to produce pseudo-data to local kafka/prometheus
* [x] Update readme with the steps to configure dev env in linux/macos

v0.5 scaling
* [x] Scale down only when no lag present
* [x] Scale only after X periods of lag/no lag
* [x] Introduce another deployment status - `SATURATED` to indicate that we don't have
    enough resources for it
* [x] Need a way to expose resource saturation level (how many CPUs are lacking)
* [x] Per-deployment auto-scaling pause

v0.6  - observability
* [x] Post behaviour updates to Kubernetes events
* [x] Cleanup logging
* [x] Expose metrics about own health and behaviour
* [x] Grafana dashboard
* [x] Update spec to deploy 3 instances of operator
* [x] Add totalMaxAllowed which will limit total number of cores available for the consumer

v0.7 testing
* [x] Guest-mode. Ability to run operator without cluster-wide permissions
* [x] Verify that scaling works with multi-container pods
* [x] Verify that disabling all auto-scaling and setting resources in the deployment itself works 
* [x] Verify that HA mode works
* [x] Verify that system operates as expected when autoscaling is disabled 
* [x] Scaling up only if there is a lag, down if there is no lag
* [x] Update owner. If operator was restarted it gets new UID, we need to update ownerRef in reconcile
* [x] [TEST] Add more integration tests 
* [x] [TEST] add test to verify that env variables are always set
* [x] [BUG] updating operator spec to scale all deployments down works, but resume doesn't

v0.8 bugfixes
* [x] Verify that system works without ResourcePolicy set
* [x] make `scaleStatePendingPeriod` configurable
* [x] profile slow reconcile (15s for ~300 deployments)
* [x] Fix statuses after the change in scaling logic (scale based on lag) 
    
v0.9 Multi-partition assignment
* [x] Static multi-partition assignment
* [x] Improve logging

vNext
* [x] Resync all metrics on reconcile. Status metric is wrong (seems to be updated only during creating of deployments)
* [ ] Implement progressive updates (canary).
* [ ] Rework configuration of RAM estimation, make it possible to provide some formula, i.e. (fixed + ramPerCore)  
* [ ] Implement `bounce` mode, keep track of the last few node names per instance and add antiaffinitity rule to deployment
      to avoid scheduling to that node during the next scale up. 
* [ ] Consider replacing DeploymentSpec with PodSpec/PodLabels/PodAnnotations
      Ability to set additional deployment-level annotations/labels  
* [ ] Recreate deployments from scratch, if any of the immutable fields were changed in the deploymentSpec
      Now, it requires manual deleting of all deployments.
* [ ] Use annotations to pause/resume configmap-based consumers

-------
Unsorted
* [ ] Consider using number of messages in all estimates instead of `projected lag time`
* [ ] Add jitter to the scaling time
* [ ] Log not only scaling cmp value (-1, 0, 1), but also how many cores were estimated per partition
* [ ] Dynamic multi-partition assignment. Instead of static `numPartitionsPerInstance`:
      Configure min/max values for `numPartitionsPerInstance`
      Configure min/max number of pods per consumer
      Based on production rate decide the value of partitions to assign. Scale each one vertically until fits.
      If per-pod resource limit is exhausted, but global one is not, scale horizontally and reduce number of partitions per instance.  
* [ ] [BUG] update of the auto-scaler spec (ratePerCore, ramPerCore) should ? trigger reconciliation
* [ ] [BUG] fix the logic for calculation hash of deploymentSpec (should always be positive) 
* [ ] Reset status annotation if MANUAL mode is enabled
* [ ] Consider making number of partitions optional in the spec
* [ ] [Feature] implement defaulting/validating webhooks
* [ ] [Feature] call external webhooks on scaling events
* [ ] [Feature] Vertical auto-scaling of balanced workloads (single deployment)
* [ ] [Feature] Fully dynamic resource allocations based on historic data
* [ ] [Feature] ? consider adding support for VPA/HPA 
* [ ] [Feature] ? Tool for operations `consumerctl stop/start consumer`
* [ ] [Feature] ? Consider getting all the pods to estimate uptime and avoid frequent restarts.
* [ ] [Feature] Implement second metrics provider (Kafka)
* [ ] [Feature] scale up without restart [blocked](https://github.com/kubernetes/kubernetes/issues/5774)
* [ ] [Feature] Get kafka lag directly from the prometheus [blocked](https://cwiki.apache.org/confluence/display/KAFKA/489%3A+Kafka+Consumer+Record+Latency+Metric)
