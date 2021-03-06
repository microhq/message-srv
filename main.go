package main

import (
	"log"
	"time"

	"github.com/micro/cli"
	"github.com/micro/go-micro"
	"github.com/microhq/message-srv/handler"
	"github.com/microhq/message-srv/message"

	"github.com/micro/go-sync/data"
	cdata "github.com/micro/go-sync/data/consul"
	"github.com/micro/go-sync/lock"
	clock "github.com/micro/go-sync/lock/consul"

	proto "github.com/microhq/message-srv/proto/message"
)

var (
	SyncAddress = "127.0.0.1:8500"
)

func main() {
	service := micro.NewService(
		micro.Name("go.micro.srv.message"),

		micro.RegisterTTL(time.Minute),
		micro.RegisterInterval(time.Second*30),

		micro.Flags(cli.StringFlag{
			Name:   "sync_address",
			EnvVar: "SYNC_ADDRESS",
			Usage:  "Address for the synchronization service e.g. consul",
		}),

		micro.Action(func(c *cli.Context) {
			if addr := c.String("sync_address"); len(addr) > 0 {
				SyncAddress = addr
			}
		}),
	)

	service.Init()

	message.Init(
		service.Server().Options().Broker,
		cdata.NewData(
			data.Nodes(SyncAddress),
			//data.Prefix("message-data"),
		),
		clock.NewLock(
			lock.Nodes(SyncAddress),
			lock.Prefix("message-lock"),
		),
	)

	proto.RegisterMessageHandler(service.Server(), new(handler.Message))

	if err := service.Run(); err != nil {
		log.Fatal(err)
	}
}
