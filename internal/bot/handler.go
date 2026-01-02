package bot

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/mahmad/slbot/internal/sl"
	"github.com/mahmad/slbot/internal/store"
)

// Handler processes Telegram messages and coordinates bot logic.
// It receives the SL client and site IDs via dependency injection.
type Handler struct {
	slClient   *sl.Client
	homeSiteID string
	workSiteID string
	userStore  *store.UserStore
	sites      []sl.Site // cached sites list

	// For button callbacks: store pending site selections
	// Maps userID to available sites for home/work selection
	pendingHome map[int64][]sl.Site
	pendingWork map[int64][]sl.Site
	mu          sync.RWMutex // protect concurrent map access
}

// NewHandler constructs a Handler.
func NewHandler(slClient *sl.Client, homeSiteID, workSiteID string, userStore *store.UserStore) *Handler {
	return &Handler{
		slClient:    slClient,
		homeSiteID:  homeSiteID,
		workSiteID:  workSiteID,
		userStore:   userStore,
		sites:       []sl.Site{},
		pendingHome: make(map[int64][]sl.Site),
		pendingWork: make(map[int64][]sl.Site),
	}
}

// HandleMessage processes a single Telegram message.
// It examines the message text and dispatches to the appropriate handler.
func (h *Handler) HandleMessage(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	// Normalize the message: lowercase and trim whitespace.
	text := strings.ToLower(strings.TrimSpace(msg.Text))

	log.Printf("User %d: %s", msg.From.ID, text)

	// Handle different commands.
	// Commands with arguments are handled via prefix matching.
	switch {
	case text == "to work":
		h.handleToWork(ctx, api, msg.Chat.ID, msg.From.ID)
	case text == "to home":
		h.handleToHome(ctx, api, msg.Chat.ID, msg.From.ID)
	case text == "/help":
		h.handleHelp(api, msg.Chat.ID)
	case text == "/prefs":
		h.handlePrefs(ctx, api, msg.Chat.ID, msg.From.ID)
	case strings.HasPrefix(text, "/sethome "):
		query := strings.TrimPrefix(text, "/sethome ")
		h.handleSetHome(ctx, api, msg.Chat.ID, msg.From.ID, query)
	case strings.HasPrefix(text, "/setwork "):
		query := strings.TrimPrefix(text, "/setwork ")
		h.handleSetWork(ctx, api, msg.Chat.ID, msg.From.ID, query)
	default:
		h.handleUnknown(api, msg.Chat.ID)
	}
}

// handleToWork fetches departures for the work site and sends them as a Telegram message.
func (h *Handler) handleToWork(ctx context.Context, api *tgbotapi.BotAPI, chatID int64, userID int64) {
	// Check user's saved work site; fall back to default (from env or constructor)
	workSiteID := h.workSiteID
	if prefs := h.userStore.GetPrefs(userID); prefs.WorkSiteID != "" {
		workSiteID = prefs.WorkSiteID
	}

	departures, err := h.slClient.GetDepartures(ctx, workSiteID)
	if err != nil {
		log.Printf("error fetching work departures: %v", err)
		h.sendMessage(api, chatID, "❌ Error fetching work departures. Try again later.")
		return
	}

	formatted := sl.FormatDepartures(departures, 3)
	message := fmt.Sprintf("🚌 Next buses to work:\n\n%s", formatted)
	h.sendMessage(api, chatID, message)
}

// handleToHome fetches departures for the home site and sends them as a Telegram message.
func (h *Handler) handleToHome(ctx context.Context, api *tgbotapi.BotAPI, chatID int64, userID int64) {
	// Check user's saved home site; fall back to default (from env or constructor)
	homeSiteID := h.homeSiteID
	if prefs := h.userStore.GetPrefs(userID); prefs.HomeSiteID != "" {
		homeSiteID = prefs.HomeSiteID
	}

	departures, err := h.slClient.GetDepartures(ctx, homeSiteID)
	if err != nil {
		log.Printf("error fetching home departures: %v", err)
		h.sendMessage(api, chatID, "❌ Error fetching home departures. Try again later.")
		return
	}

	formatted := sl.FormatDepartures(departures, 3)
	message := fmt.Sprintf("🚌 Next buses to home:\n\n%s", formatted)
	h.sendMessage(api, chatID, message)
}

// handleHelp sends the help message listing all available commands.
func (h *Handler) handleHelp(api *tgbotapi.BotAPI, chatID int64) {
	help := `Available commands:
• to work - Next buses to work
• to home - Next buses to home
• /sethome <location> - Set your home bus stop
• /setwork <location> - Set your work bus stop
• /prefs - Show saved home/work preferences
• /help - Show this message`
	h.sendMessage(api, chatID, help)
}

// handleSetHome prompts the user to select their home stop.
func (h *Handler) handleSetHome(ctx context.Context, api *tgbotapi.BotAPI, chatID int64, userID int64, query string) {
	if query == "" {
		h.sendMessage(api, chatID, "❓ Usage: /sethome <location name>")
		return
	}

	log.Printf("handleSetHome: user=%d query=%q cached_sites=%d", userID, query, len(h.sites))

	// Load sites if not already cached.
	if len(h.sites) == 0 {
		sites, err := h.slClient.GetSites(ctx)
		if err != nil {
			log.Printf("handleSetHome: error fetching sites: %v", err)
			h.sendMessage(api, chatID, "❌ Error fetching sites. Try again later.")
			return
		}
		h.sites = sites
		log.Printf("handleSetHome: fetched %d sites", len(h.sites))
		// Log site list (limit to first 200 entries)
		max := len(h.sites)
		if max > 200 {
			max = 200
		}
		for i := 0; i < max; i++ {
			s := h.sites[i]
			log.Printf("handleSetHome: site %d: %s (id=%d)", i, s.Name, s.SiteID)
		}
	}

	// Fuzzy match the query.
	matches := sl.FuzzyMatch(query, h.sites, 3)
	log.Printf("handleSetHome: matches=%d", len(matches))
	if len(matches) == 0 {
		h.sendMessage(api, chatID, fmt.Sprintf("❌ No sites found matching '%s'", query))
		return
	}

	// Log match details
	for i, m := range matches {
		log.Printf("handleSetHome: match %d: %s (site %d)", i, m.Name, m.SiteID)
	}

	// If only one match, save it directly
	if len(matches) == 1 {
		selected := matches[0]
		if err := h.userStore.SetHome(userID, fmt.Sprintf("%d", selected.SiteID)); err != nil {
			log.Printf("handleSetHome: error setting home: %v", err)
			h.sendMessage(api, chatID, "❌ Error saving preference. Try again later.")
			return
		}
		log.Printf("handleSetHome: saved home site %d for user %d", selected.SiteID, userID)
		h.sendMessage(api, chatID, fmt.Sprintf("✅ Home set to: %s", selected.Name))
		return
	}

	// Multiple matches: store and show buttons
	h.mu.Lock()
	h.pendingHome[userID] = matches
	h.mu.Unlock()

	// Create inline buttons for each match
	var buttons [][]tgbotapi.InlineKeyboardButton
	for _, site := range matches {
		button := tgbotapi.NewInlineKeyboardButtonData(
			site.Name,
			fmt.Sprintf("home_%d_%d", userID, site.SiteID),
		)
		buttons = append(buttons, []tgbotapi.InlineKeyboardButton{button})
	}

	markup := tgbotapi.NewInlineKeyboardMarkup(buttons...)
	msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("Multiple matches for '%s'. Which one?", query))
	msg.ReplyMarkup = markup
	if _, err := api.Send(msg); err != nil {
		log.Printf("handleSetHome: error sending button message: %v", err)
	}
}

// handleSetWork prompts the user to select their work stop.
func (h *Handler) handleSetWork(ctx context.Context, api *tgbotapi.BotAPI, chatID int64, userID int64, query string) {
	if query == "" {
		h.sendMessage(api, chatID, "❓ Usage: /setwork <location name>")
		return
	}

	log.Printf("handleSetWork: user=%d query=%q cached_sites=%d", userID, query, len(h.sites))

	// Load sites if not already cached.
	if len(h.sites) == 0 {
		sites, err := h.slClient.GetSites(ctx)
		if err != nil {
			log.Printf("handleSetWork: error fetching sites: %v", err)
			h.sendMessage(api, chatID, "❌ Error fetching sites. Try again later.")
			return
		}
		h.sites = sites
		log.Printf("handleSetWork: fetched %d sites", len(h.sites))
		// Log site list (limit to first 200 entries)
		max := len(h.sites)
		if max > 200 {
			max = 200
		}
		for i := 0; i < max; i++ {
			s := h.sites[i]
			log.Printf("handleSetWork: site %d: %s (id=%d)", i, s.Name, s.SiteID)
		}
	}

	// Fuzzy match the query.
	matches := sl.FuzzyMatch(query, h.sites, 3)
	log.Printf("handleSetWork: matches=%d", len(matches))
	if len(matches) == 0 {
		h.sendMessage(api, chatID, fmt.Sprintf("❌ No sites found matching '%s'", query))
		return
	}

	for i, m := range matches {
		log.Printf("handleSetWork: match %d: %s (site %d)", i, m.Name, m.SiteID)
	}

	// If only one match, save it directly
	if len(matches) == 1 {
		selected := matches[0]
		if err := h.userStore.SetWork(userID, fmt.Sprintf("%d", selected.SiteID)); err != nil {
			log.Printf("handleSetWork: error setting work: %v", err)
			h.sendMessage(api, chatID, "❌ Error saving preference. Try again later.")
			return
		}
		log.Printf("handleSetWork: saved work site %d for user %d", selected.SiteID, userID)
		h.sendMessage(api, chatID, fmt.Sprintf("✅ Work set to: %s", selected.Name))
		return
	}

	// Multiple matches: store and show buttons
	h.mu.Lock()
	h.pendingWork[userID] = matches
	h.mu.Unlock()

	// Create inline buttons for each match
	var buttons [][]tgbotapi.InlineKeyboardButton
	for _, site := range matches {
		button := tgbotapi.NewInlineKeyboardButtonData(
			site.Name,
			fmt.Sprintf("work_%d_%d", userID, site.SiteID),
		)
		buttons = append(buttons, []tgbotapi.InlineKeyboardButton{button})
	}

	markup := tgbotapi.NewInlineKeyboardMarkup(buttons...)
	msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("Multiple matches for '%s'. Which one?", query))
	msg.ReplyMarkup = markup
	if _, err := api.Send(msg); err != nil {
		log.Printf("handleSetWork: error sending button message: %v", err)
	}
}

// handlePrefs shows the current saved preferences for the user.
func (h *Handler) handlePrefs(ctx context.Context, api *tgbotapi.BotAPI, chatID int64, userID int64) {
	prefs := h.userStore.GetPrefs(userID)

	homeSite := prefs.HomeSiteID
	homeNote := "(saved)"
	if homeSite == "" {
		homeSite = h.homeSiteID
		homeNote = "(default)"
	}
	homeName := h.siteNameByID(ctx, homeSite)

	workSite := prefs.WorkSiteID
	workNote := "(saved)"
	if workSite == "" {
		workSite = h.workSiteID
		workNote = "(default)"
	}
	workName := h.siteNameByID(ctx, workSite)

	msg := fmt.Sprintf("Your preferences:\nHome: %s %s (site %s)\nWork: %s %s (site %s)\n\nChange with /sethome <name> and /setwork <name>",
		homeName, homeNote, homeSite, workName, workNote, workSite)

	h.sendMessage(api, chatID, msg)
}

// siteNameByID returns a friendly site name for a site ID string.
// It ensures the handler's site cache is populated.
func (h *Handler) siteNameByID(ctx context.Context, siteID string) string {
	if len(h.sites) == 0 {
		sites, err := h.slClient.GetSites(ctx)
		if err == nil {
			h.sites = sites
		}
	}

	// siteID is stored as string; SL Site.SiteID is int
	id, err := strconv.Atoi(siteID)
	if err == nil {
		for _, s := range h.sites {
			if s.SiteID == id {
				return s.Name
			}
		}
	}

	// Fallback: return the raw siteID
	return siteID
}

// handleUnknown sends a message when the user sends an unrecognized command.
func (h *Handler) handleUnknown(api *tgbotapi.BotAPI, chatID int64) {
	h.sendMessage(api, chatID, "❓ Unknown command. Type /help for available commands.")
}

// HandleCallback processes inline button callbacks (site selection).
// Expected callback data format: "home_<userID>_<siteID>" or "work_<userID>_<siteID>"
func (h *Handler) HandleCallback(ctx context.Context, api *tgbotapi.BotAPI, callback *tgbotapi.CallbackQuery) {
	data := callback.Data
	parts := strings.Split(data, "_")
	if len(parts) != 3 {
		log.Printf("HandleCallback: invalid data format: %s", data)
		return
	}

	action := parts[0]
	userID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		log.Printf("HandleCallback: invalid userID: %s", parts[1])
		return
	}
	siteID, err := strconv.Atoi(parts[2])
	if err != nil {
		log.Printf("HandleCallback: invalid siteID: %s", parts[2])
		return
	}

	var siteName string

	if action == "home" {
		h.mu.RLock()
		matches := h.pendingHome[userID]
		h.mu.RUnlock()

		// Find the selected site by ID
		for _, site := range matches {
			if site.SiteID == siteID {
				siteName = site.Name
				break
			}
		}

		if siteName == "" {
			h.sendMessage(api, callback.Message.Chat.ID, "❌ Site not found in pending selections.")
			return
		}

		// Save the preference
		if err := h.userStore.SetHome(userID, fmt.Sprintf("%d", siteID)); err != nil {
			log.Printf("HandleCallback: error setting home: %v", err)
			h.sendMessage(api, callback.Message.Chat.ID, "❌ Error saving preference.")
			return
		}

		// Clean up pending
		h.mu.Lock()
		delete(h.pendingHome, userID)
		h.mu.Unlock()

		// Edit the message to show confirmation
		edit := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID,
			fmt.Sprintf("✅ Home set to: %s", siteName))
		if _, err := api.Send(edit); err != nil {
			log.Printf("HandleCallback: error editing message: %v", err)
		}
		log.Printf("HandleCallback: saved home site %d (%s) for user %d", siteID, siteName, userID)

	} else if action == "work" {
		h.mu.RLock()
		matches := h.pendingWork[userID]
		h.mu.RUnlock()

		// Find the selected site by ID
		for _, site := range matches {
			if site.SiteID == siteID {
				siteName = site.Name
				break
			}
		}

		if siteName == "" {
			h.sendMessage(api, callback.Message.Chat.ID, "❌ Site not found in pending selections.")
			return
		}

		// Save the preference
		if err := h.userStore.SetWork(userID, fmt.Sprintf("%d", siteID)); err != nil {
			log.Printf("HandleCallback: error setting work: %v", err)
			h.sendMessage(api, callback.Message.Chat.ID, "❌ Error saving preference.")
			return
		}

		// Clean up pending
		h.mu.Lock()
		delete(h.pendingWork, userID)
		h.mu.Unlock()

		// Edit the message to show confirmation
		edit := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID,
			fmt.Sprintf("✅ Work set to: %s", siteName))
		if _, err := api.Send(edit); err != nil {
			log.Printf("HandleCallback: error editing message: %v", err)
		}
		log.Printf("HandleCallback: saved work site %d (%s) for user %d", siteID, siteName, userID)
	}
}

// sendMessage is a helper to send a Telegram message.
// It abstracts away the tgbotapi boilerplate.
func (h *Handler) sendMessage(api *tgbotapi.BotAPI, chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown" // enable markdown formatting later
	if _, err := api.Send(msg); err != nil {
		log.Printf("error sending message: %v", err)
	}
}

// SetSites allows injecting a pre-fetched list of sites into the handler (used at startup).
func (h *Handler) SetSites(sites []sl.Site) {
	h.sites = sites
}
