package main

import (
	"flag"

	"github.com/ditcraft/demo-validator/ethereum"
	"github.com/golang/glog"
	"github.com/joho/godotenv"
)

func main() {
	flag.Set("alsologtostderr", "true")
	flag.Set("log_dir", "./log")
	flag.Set("v", "0")
	flag.Parse()
	glog.Info("Starting ditCraft demo validator...")

	err := godotenv.Load()
	if err != nil {
		glog.Fatal("Error loading .env file")
	}

	go ethereum.WatchEvents()

	err = ethereum.Approve()
	if err != nil {
		glog.Fatal(err)
	} else {
		glog.Info("Approvals successful")
	}

	select {}
}
