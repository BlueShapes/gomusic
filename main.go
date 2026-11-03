package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/disgoorg/disgo/handler"

	"github.com/blueshapes/gomusic/internal/bot"
	"github.com/blueshapes/gomusic/internal/command"
	"github.com/blueshapes/gomusic/internal/config"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load("config.toml")
	if errors.Is(err, config.ErrCreated) {
		fmt.Println("config.toml を作成しました。中身を設定して再起動してください。")
		return nil
	}
	if err != nil {
		return err
	}

	b, err := bot.New(cfg, slog.Default())
	if err != nil {
		return err
	}

	b.Client.AddEventListeners(command.Register(b), b)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := handler.SyncCommands(b.Client, command.Definitions(), b.GuildIDs()); err != nil {
		return fmt.Errorf("sync commands: %w", err)
	}
	if err := b.Open(ctx); err != nil {
		return fmt.Errorf("open gateway: %w", err)
	}
	slog.Info("musicord is running", "guilds", b.GuildIDs())

	<-ctx.Done()
	slog.Info("shutting down")

	closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	b.Close(closeCtx)
	return nil
}
