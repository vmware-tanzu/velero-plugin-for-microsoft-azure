module github.com/vmware-tanzu/velero-plugin-for-microsoft-azure

go 1.13

require (
	github.com/Azure/azure-sdk-for-go v55.1.0+incompatible
	github.com/Azure/go-autorest/autorest v0.11.19
	github.com/Azure/go-autorest/autorest/azure/auth v0.5.7
	github.com/dnaeon/go-vcr v1.0.1 // indirect
	github.com/hashicorp/go-hclog v0.9.2 // indirect
	github.com/hashicorp/go-plugin v1.0.1-0.20190610192547-a1bc61569a26 // indirect
	github.com/hashicorp/yamux v0.0.0-20190923154419-df201c70410d // indirect
	github.com/joho/godotenv v1.3.0
	github.com/pkg/errors v0.9.1
	github.com/satori/go.uuid v1.2.0
	github.com/sirupsen/logrus v1.8.1
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.7.0
	github.com/vmware-tanzu/velero v1.6.0
	k8s.io/api v0.19.7
	k8s.io/apimachinery v0.19.7
)
