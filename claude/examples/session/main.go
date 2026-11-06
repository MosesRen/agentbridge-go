package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/MosesRen/agentbridge-go/claude"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	client, err := claude.Dial(ctx, claude.Options{
		SettingSources: []string{"user"}, Tools: []string{},
		StrictMCPConfig: true, MCPConfig: []byte(`{"mcpServers":{}}`),
		NoSessionPersistence: true, MaxTurns: 1,
	})
	if err != nil {
		return err
	}
	defer client.Close()
	for _, prompt := range []string{
		"Remember the code word ORCHID. Reply only READY.",
		"What code word did I ask you to remember? Reply only that word.",
	} {
		if err := client.Send(ctx, prompt); err != nil {
			return err
		}
		for {
			message, err := client.Receive(ctx)
			if err != nil {
				return err
			}
			if message.Type == "result" {
				result, err := message.Result()
				if err != nil {
					return err
				}
				fmt.Println(result.Result)
				break
			}
		}
	}
	return nil
}
