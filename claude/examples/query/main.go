package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"time"

	"agentbridge-go/claude"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: go run ./examples/query 'your prompt'")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	stream, err := claude.Query(ctx, strings.Join(os.Args[1:], " "), claude.Options{
		Dir: ".", SettingSources: []string{"user"}, MaxTurns: 10,
	})
	if err != nil {
		log.Fatal(err)
	}
	// Run in a function so deferred Close executes before log.Fatal exits.
	if err := consume(ctx, stream); err != nil {
		log.Fatal(err)
	}
}

func consume(ctx context.Context, stream *claude.Stream) error {
	defer stream.Close()
	for {
		message, err := stream.Next(ctx)
		if err != nil {
			return err
		}
		if message.Type == "assistant" && message.Text() != "" {
			fmt.Println(message.Text())
		}
		if message.Type == "result" {
			_, err := stream.Result(ctx)
			return err
		}
	}
}
