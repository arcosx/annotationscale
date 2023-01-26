# Example

```shell
kubectl apply -f nginx-deployment.yaml
```

scale up
```shell
go run main.go scaleup 
```

releasing
```shell
go run main.go releasing
```

scale down
```shell
go run main.go scaledown
```

# Output
kubectl get events --field-selector involvedObject.name=nginx-deployment -w
* scale up : 1 --> 2 --> 5 --> 8 --> 10(stop point)
* releasing: 10 -> 12
* scale down 12 -> 5 -> 1

```shell
26s         Normal   ScalingReplicaSet   deployment/nginx-deployment   Scaled up replica set nginx-deployment-85996f8dbd to 2 from 1
0s          Normal   ScalingReplicaSet   deployment/nginx-deployment   Scaled up replica set nginx-deployment-85996f8dbd to 5 from 2
0s          Normal   ScalingReplicaSet   deployment/nginx-deployment   Scaled up replica set nginx-deployment-85996f8dbd to 8 from 5
0s          Normal   ScalingReplicaSet   deployment/nginx-deployment   Scaled up replica set nginx-deployment-85996f8dbd to 10 from 8
0s          Normal   ScalingReplicaSet   deployment/nginx-deployment   Scaled up replica set nginx-deployment-85996f8dbd to 12 from 10
0s          Normal   ScalingReplicaSet   deployment/nginx-deployment   Scaled down replica set nginx-deployment-85996f8dbd to 5 from 12
0s          Normal   ScalingReplicaSet   deployment/nginx-deployment   Scaled down replica set nginx-deployment-85996f8dbd to 1 from 5
```