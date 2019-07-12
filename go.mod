module github.com/zenoss/zenoss-agent-kubernetes

go 1.12

require (
	github.com/golang/protobuf v1.3.2
	github.com/gophercloud/gophercloud v0.2.0 // indirect
	github.com/imdario/mergo v0.3.7 // indirect
	github.com/sirupsen/logrus v1.4.2
	github.com/spf13/pflag v1.0.3
	github.com/spf13/viper v1.4.0
	github.com/zenoss/zenoss-protobufs v0.0.0-20190429202757-89476027a2e4
	google.golang.org/genproto v0.0.0-20190307195333-5fe7a883aa19 // indirect
	google.golang.org/grpc v1.22.0
	k8s.io/client-go v0.0.0-20190711103903-4a0861cac5e0
	k8s.io/metrics v0.0.0-20190711111147-f6e53fe91d3b
)
