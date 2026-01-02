package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/mahmad/slbot/internal/bot"
	"github.com/mahmad/slbot/internal/sl"
	"github.com/mahmad/slbot/internal/store"
)

// Config holds all runtime configuration from environment variables.
// This is a common pattern in Go: gather all config in one struct.
type Config struct {
	TelegramBotToken string
	HomeSiteID       string
	WorkSiteID       string
	DryRun           bool
}

// loadConfig reads environment variables and returns a Config struct.
// In Go, we often prefer explicit error handling and early returns.
func loadConfig() (Config, error) {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		return Config{}, fmt.Errorf("TELEGRAM_BOT_TOKEN not set")
	}

	homeSiteID := os.Getenv("HOME_SITE_ID")
	if homeSiteID == "" {
		return Config{}, fmt.Errorf("HOME_SITE_ID not set")
	}

	workSiteID := os.Getenv("WORK_SITE_ID")
	if workSiteID == "" {
		return Config{}, fmt.Errorf("WORK_SITE_ID not set")
	}

	dryRun := os.Getenv("SL_DRY_RUN") == "1"

	return Config{
		TelegramBotToken: token,
		HomeSiteID:       homeSiteID,
		WorkSiteID:       workSiteID,
		DryRun:           dryRun,
	}, nil
}

func main() {
	// Load configuration from environment.
	// In Go, we handle errors eagerly. If loadConfig fails, log and exit.
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	// Create a reusable HTTP client with timeouts.
	// Go's http.DefaultClient has no timeouts, which can hang your bot.
	// Always configure explicit timeouts for production.
	httpClient := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        10,
			MaxIdleConnsPerHost: 5,
			IdleConnTimeout:     30 * time.Second,
		},
	}

	// Create the SL API client.
	// This is DEPENDENCY INJECTION: the SL client receives its dependencies (httpClient, dryRun flag).
	// This makes testing easy: you can swap out a fake HTTP client for tests.
	slClient := sl.NewClient(httpClient, cfg.DryRun)

	// Create the Telegram bot API.
	api, err := tgbotapi.NewBotAPI(cfg.TelegramBotToken)
	if err != nil {
		log.Fatalf("telegram error: %v", err)
	}
	api.Debug = false // set to true for verbose Telegram logs

	log.Printf("Authorized on account @%s\n", api.Self.UserName)

	// Create the user preference store with file persistence.
	// This stores home/work site IDs per user and persists to disk.
	userStore := store.NewUserStore("data/userprefs.json")

	// Create the bot handler.
	// The handler receives the SL client, user site IDs, and the user store.
	// This separation lets you test handler logic without touching Telegram.
	handler := bot.NewHandler(slClient, cfg.HomeSiteID, cfg.WorkSiteID, userStore)

	// Fetch sites on startup and persist a cache to disk for debugging/fast startup.
	ctxSites, cancelSites := context.WithTimeout(context.Background(), 10*time.Second)
	sites, err := slClient.GetSites(ctxSites)
	cancelSites()
	if err != nil {
		log.Printf("warning: could not fetch sites on startup: %v", err)
	} else {
		handler.SetSites(sites)
		// Ensure data directory exists and write sites cache.
		cachePath := "data/sites_cache.json"
		if dir := filepath.Dir(cachePath); dir != "." {
			_ = os.MkdirAll(dir, 0o755)
		}
		if b, err := json.MarshalIndent(sites, "", "  "); err != nil {
			log.Printf("error marshalling sites for cache: %v", err)
		} else if err := os.WriteFile(cachePath, b, 0644); err != nil {
			log.Printf("error writing sites cache: %v", err)
		} else {
			log.Printf("wrote %d sites to %s", len(sites), cachePath)
		}
	}

	// Set up Telegram long polling.
	// Long polling: bot repeatedly asks Telegram "any new messages for me?"
	// This avoids needing a public webhook URL for local development.
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	// GetUpdatesChan returns a channel that receives new Telegram updates.
	// Channels are Go's way of safely passing data between concurrent functions (goroutines).
	updates := api.GetUpdatesChan(u)

	log.Println("Bot is running. Send /help for commands.")

	// Main loop: wait for and process each Telegram message.
	// range on a channel will block until a message arrives.
	for update := range updates {
		// Handle messages
		if update.Message != nil {
			// Create a context with a 30-second timeout for this update.
			// context.Context is Go's standard way to handle cancellation and deadlines.
			// Passing it down lets every function (API calls, formatting, etc.) respect the timeout.
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

			// Call the handler to process the message.
			handler.HandleMessage(ctx, api, update.Message)

			// Always cancel contexts to release resources.
			// This is idiomatic Go: use defer to ensure cleanup happens even if the code panics.
			cancel()
		}

		// Handle callback queries (button clicks)
		if update.CallbackQuery != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			handler.HandleCallback(ctx, api, update.CallbackQuery)
			cancel()

			// Acknowledge the callback
			callback := tgbotapi.NewCallback(update.CallbackQuery.ID, "")
			if _, err := api.Request(callback); err != nil {
				log.Printf("error acknowledging callback: %v", err)
			}
		}
	}
}
