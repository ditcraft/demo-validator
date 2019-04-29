# ditCraft Demo Validator
Users who are testing the dit client in demo mode would be pretty bored if they propose a commit and no one votes on it. This is why we created a simple demo validator, written in Go. It waits for the ProposeCommit event from the ditCoordinator (only the demo one) and automatically votes on it with five demo voters. Their choices are random.

## Running it yourself
You like the idea and want to use a demo validator in your own project? Go ahead - we embrace open source!

* Run `go get github.com/ditcraft/demo-validator`
    * Note: Since this is a go project, golang-go needs to be installed
* Enter the directory of the demo-validator with `cd $GOPATH/src/github.com/ditcraft/demo-validator`
* Install the necessary dependencies with `go get -d ./...`
* Run with `go run main.go`

Additionally, in order to work, this demo validator needs an .env file in the same directory, containing the following things:

```
ETHEREUM_RPC=https://my.fancy.rpc.url

MONGO_DB_ADDRESS=127.0.0.1:27017

CONTRACT_DIT_COORDINATOR='0x0000000000000000000000000000000000000000'
CONTRACT_DIT_TOKEN='0x0000000000000000000000000000000000000000'
CONTRACT_KNW_VOTING='0x0000000000000000000000000000000000000000'
CONTRACT_KNW_TOKEN='0x0000000000000000000000000000000000000000'

ETHEREUM_ADDRESS_0='0x0000000000000000000000000000000000000000'
ETHEREUM_ADDRESS_1='0x0000000000000000000000000000000000000000'
ETHEREUM_ADDRESS_2='0x0000000000000000000000000000000000000000'
ETHEREUM_ADDRESS_3='0x0000000000000000000000000000000000000000'
ETHEREUM_ADDRESS_4='0x0000000000000000000000000000000000000000'

ETHEREUM_PK_0='0000000000000000000000000000000000000000000000000000000000000000'
ETHEREUM_PK_1='0000000000000000000000000000000000000000000000000000000000000000'
ETHEREUM_PK_2='0000000000000000000000000000000000000000000000000000000000000000'
ETHEREUM_PK_3='0000000000000000000000000000000000000000000000000000000000000000'
ETHEREUM_PK_4='0000000000000000000000000000000000000000000000000000000000000000'
```