package bot

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/mahmad/slbot/internal/journeyplanner"
	"github.com/mahmad/slbot/internal/store"
)

// Handler processes Telegram messages and coordinates bot logic.
// It receives the Journey Planner client and default locations via dependency injection.
type Handler struct {
	jpClient   *journeyplanner.Client
	homeSiteID string
	workSiteID string
	userStore  *store.UserStore

	// For button callbacks: store pending location selections.
	pendingHome map[int64][]journeyplanner.Location
	pendingWork map[int64][]journeyplanner.Location
	mu          sync.RWMutex // protect concurrent map access
}

// NewHandler constructs a Handler.
func NewHandler(jpClient *journeyplanner.Client, homeSiteID, workSiteID string, userStore *store.UserStore) *Handler {
	return &Handler{
		jpClient:    jpClient,
		homeSiteID:  homeSiteID,
		workSiteID:  workSiteID,
		userStore:   userStore,
		pendingHome: make(map[int64][]journeyplanner.Location),
		pendingWork: make(map[int64][]journeyplanner.Location),
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
	case strings.HasPrefix(text, "/sethome"):
		query := strings.TrimPrefix(text, "/sethome")
		query = strings.TrimSpace(query)
		h.handleSetHome(ctx, api, msg.Chat.ID, msg.From.ID, query)
	case strings.HasPrefix(text, "/setwork"):
		query := strings.TrimPrefix(text, "/setwork")
		query = strings.TrimSpace(query)
		h.handleSetWork(ctx, api, msg.Chat.ID, msg.From.ID, query)
	case text == "/setpriority":
		h.handleSetPriority(ctx, api, msg.Chat.ID, msg.From.ID)
	default:
		h.handleUnknown(api, msg.Chat.ID)
	}
}

// handleToWork fetches departures for the work site and sends them as a Telegram message.
func (h *Handler) handleToWork(ctx context.Context, api *tgbotapi.BotAPI, chatID int64, userID int64) {
	prefs := h.userStore.GetPrefs(userID)
	origin := prefs.HomeLocation
	if origin == "" {
		origin = h.homeSiteID
	}
	destination := prefs.WorkLocation
	if destination == "" {
		destination = h.workSiteID
	}

	if origin == "" || destination == "" {
		h.sendMessage(api, chatID, "❓ Set your locations first: /sethome <location> and /setwork <location>.")
		return
	}

	routeType := prefs.RoutePriority

	journeys, err := h.jpClient.Trips(ctx, origin, destination, 3, routeType)
	if err != nil {
		log.Printf("error fetching trips home->work: %v", err)
		h.sendMessage(api, chatID, "❌ Error fetching trips. Try again later.")
		return
	}

	originLabel := h.locationLabel(ctx, origin)
	destinationLabel := h.locationLabel(ctx, destination)
	formatted := journeyplanner.FormatJourneys(originLabel, destinationLabel, journeys, 3)
	h.sendMessage(api, chatID, formatted)
}

// handleToHome fetches departures for the home site and sends them as a Telegram message.
func (h *Handler) handleToHome(ctx context.Context, api *tgbotapi.BotAPI, chatID int64, userID int64) {
	prefs := h.userStore.GetPrefs(userID)
	origin := prefs.WorkLocation
	if origin == "" {
		origin = h.workSiteID
	}
	destination := prefs.HomeLocation
	if destination == "" {
		destination = h.homeSiteID
	}

	if origin == "" || destination == "" {
		h.sendMessage(api, chatID, "❓ Set your locations first: /sethome <location> and /setwork <location>.")
		return
	}

	routeType := prefs.RoutePriority

	journeys, err := h.jpClient.Trips(ctx, origin, destination, 3, routeType)
	if err != nil {
		log.Printf("error fetching trips work->home: %v", err)
		h.sendMessage(api, chatID, "❌ Error fetching trips. Try again later.")
		return
	}

	originLabel := h.locationLabel(ctx, origin)
	destinationLabel := h.locationLabel(ctx, destination)
	formatted := journeyplanner.FormatJourneys(originLabel, destinationLabel, journeys, 3)
	h.sendMessage(api, chatID, formatted)
}

// handleHelp sends the help message listing all available commands.
func (h *Handler) handleHelp(api *tgbotapi.BotAPI, chatID int64) {
	help := `Available commands:
• to work - Next trips from home to work
• to home - Next trips from work to home
• /sethome <location> - Set your home location
• /setwork <location> - Set your work location
• /setpriority - Set route preference (fastest (default) / least transfers / least walking)
• /prefs - Show saved locations and preferences
• /help - Show this message

Location examples:
• Address: "Drottninggatan 1, Stockholm"
• Stop name: "Odenplan"
• Coordinates: "18.013809:59.335104:WGS84[dd.ddddd]"`
	h.sendMessage(api, chatID, help)
}

// handleSetHome prompts the user to select their home stop.
func (h *Handler) handleSetHome(ctx context.Context, api *tgbotapi.BotAPI, chatID int64, userID int64, query string) {
	if query == "" {
		h.sendMessage(api, chatID, "❓ Usage: /sethome <location>\n\nExamples:\n• /sethome Odenplan\n• /sethome Drottninggatan 1, Stockholm")
		return
	}

	log.Printf("handleSetHome: user=%d query=%q", userID, query)

	locs, err := h.jpClient.StopFinder(ctx, query, 5)
	if err != nil {
		log.Printf("handleSetHome: stop-finder error: %v", err)
		h.sendMessage(api, chatID, "❌ Error searching locations. Try again later.")
		return
	}
	if len(locs) == 0 {
		h.sendMessage(api, chatID, fmt.Sprintf("❌ No locations found matching '%s'", query))
		return
	}

	if len(locs) == 1 {
		selected := locs[0]
		if err := h.userStore.SetHome(userID, selected.ID); err != nil {
			log.Printf("handleSetHome: error setting home: %v", err)
			h.sendMessage(api, chatID, "❌ Error saving preference. Try again later.")
			return
		}
		h.sendMessage(api, chatID, fmt.Sprintf("✅ Home set to: %s", selected.Name))
		return
	}

	h.mu.Lock()
	h.pendingHome[userID] = locs
	h.mu.Unlock()

	var buttons [][]tgbotapi.InlineKeyboardButton
	for i, loc := range locs {
		label := fmt.Sprintf("%s (%s)", loc.Name, loc.Type)
		button := tgbotapi.NewInlineKeyboardButtonData(
			label,
			fmt.Sprintf("homejp_%d_%d", userID, i),
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
		h.sendMessage(api, chatID, "❓ Usage: /setwork <location>\n\nExamples:\n• /setwork Slussen\n• /setwork Kungsgatan 10, Stockholm")
		return
	}

	log.Printf("handleSetWork: user=%d query=%q", userID, query)

	locs, err := h.jpClient.StopFinder(ctx, query, 5)
	if err != nil {
		log.Printf("handleSetWork: stop-finder error: %v", err)
		h.sendMessage(api, chatID, "❌ Error searching locations. Try again later.")
		return
	}
	if len(locs) == 0 {
		h.sendMessage(api, chatID, fmt.Sprintf("❌ No locations found matching '%s'", query))
		return
	}

	if len(locs) == 1 {
		selected := locs[0]
		if err := h.userStore.SetWork(userID, selected.ID); err != nil {
			log.Printf("handleSetWork: error setting work: %v", err)
			h.sendMessage(api, chatID, "❌ Error saving preference. Try again later.")
			return
		}
		h.sendMessage(api, chatID, fmt.Sprintf("✅ Work set to: %s", selected.Name))
		return
	}

	h.mu.Lock()
	h.pendingWork[userID] = locs
	h.mu.Unlock()

	var buttons [][]tgbotapi.InlineKeyboardButton
	for i, loc := range locs {
		label := fmt.Sprintf("%s (%s)", loc.Name, loc.Type)
		button := tgbotapi.NewInlineKeyboardButtonData(
			label,
			fmt.Sprintf("workjp_%d_%d", userID, i),
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

	homeLocation := prefs.HomeLocation
	homeNote := "(saved)"
	if homeLocation == "" {
		homeLocation = h.homeSiteID
		homeNote = "(default)"
	}

	workLocation := prefs.WorkLocation
	workNote := "(saved)"
	if workLocation == "" {
		workLocation = h.workSiteID
		workNote = "(default)"
	}

	homeLocation = h.locationLabel(ctx, homeLocation)
	workLocation = h.locationLabel(ctx, workLocation)

	priority := prefs.RoutePriority
	priorityNote := ""
	if priority == "" {
		priority = "Not set (using default)"
	} else {
		priorityNote = "(saved)"
		switch priority {
		case "leastinterchange":
			priority = "Least transfers"
		case "leasttime":
			priority = "Fastest"
		case "leastwalking":
			priority = "Least walking"
		}
	}

	msg := fmt.Sprintf("Your preferences:\nHome: %s %s\nWork: %s %s\nRoute priority: %s %s\n\nChange with /sethome, /setwork, and /setpriority",
		homeLocation, homeNote, workLocation, workNote, priority, priorityNote)

	h.sendMessage(api, chatID, msg)
}

func (h *Handler) locationLabel(ctx context.Context, location string) string {
	location = strings.TrimSpace(location)
	if location == "" {
		return ""
	}
	locs, err := h.jpClient.StopFinder(ctx, location, 1)
	if err != nil || len(locs) == 0 {
		return location
	}
	if locs[0].Name == "" {
		return location
	}
	return locs[0].Name
}

// handleSetPriority shows inline buttons for the user to select route priority.
func (h *Handler) handleSetPriority(ctx context.Context, api *tgbotapi.BotAPI, chatID int64, userID int64) {
	buttons := [][]tgbotapi.InlineKeyboardButton{
		{tgbotapi.NewInlineKeyboardButtonData("⚡ Fastest", fmt.Sprintf("priority_%d_leasttime", userID))},
		{tgbotapi.NewInlineKeyboardButtonData("🔄 Least transfers", fmt.Sprintf("priority_%d_leastinterchange", userID))},
		{tgbotapi.NewInlineKeyboardButtonData("🚶 Least walking", fmt.Sprintf("priority_%d_leastwalking", userID))},
	}

	markup := tgbotapi.NewInlineKeyboardMarkup(buttons...)
	msg := tgbotapi.NewMessage(chatID, "Choose your route priority:")
	msg.ReplyMarkup = markup
	if _, err := api.Send(msg); err != nil {
		log.Printf("handleSetPriority: error sending button message: %v", err)
	}
}

// handleUnknown sends a message when the user sends an unrecognized command.
func (h *Handler) handleUnknown(api *tgbotapi.BotAPI, chatID int64) {
	h.sendMessage(api, chatID, "❓ Unknown command. Type /help for available commands.")
}

// HandleCallback processes inline button callbacks (site selection).
// Expected callback data format: "home_<userID>_<siteID>" or "work_<userID>_<siteID>" or "priority_<userID>_<value>"
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

	// Handle priority callbacks separately (third part is a string, not an index)
	if action == "priority" {
		priorityValue := parts[2]

		if err := h.userStore.SetPriority(userID, priorityValue); err != nil {
			log.Printf("HandleCallback: error setting priority: %v", err)
			h.sendMessage(api, callback.Message.Chat.ID, "❌ Error saving preference.")
			return
		}

		priorityLabel := ""
		switch priorityValue {
		case "leasttime":
			priorityLabel = "Fastest"
		case "leastinterchange":
			priorityLabel = "Least transfers"
		case "leastwalking":
			priorityLabel = "Least walking"
		}

		edit := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID,
			fmt.Sprintf("✅ Route priority set to: %s", priorityLabel))
		if _, err := api.Send(edit); err != nil {
			log.Printf("HandleCallback: error editing message: %v", err)
		}
		log.Printf("HandleCallback: saved priority %q for user %d", priorityValue, userID)
		return
	}

	// For location callbacks, parse the third part as an index
	selectionIndex, err := strconv.Atoi(parts[2])
	if err != nil {
		log.Printf("HandleCallback: invalid selection index: %s", parts[2])
		return
	}

	if action == "homejp" {
		h.mu.RLock()
		matches := h.pendingHome[userID]
		h.mu.RUnlock()

		if selectionIndex < 0 || selectionIndex >= len(matches) {
			h.sendMessage(api, callback.Message.Chat.ID, "❌ Location not found in pending selections.")
			return
		}
		selected := matches[selectionIndex]

		if err := h.userStore.SetHome(userID, selected.ID); err != nil {
			log.Printf("HandleCallback: error setting home: %v", err)
			h.sendMessage(api, callback.Message.Chat.ID, "❌ Error saving preference.")
			return
		}

		h.mu.Lock()
		delete(h.pendingHome, userID)
		h.mu.Unlock()

		edit := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID,
			fmt.Sprintf("✅ Home set to: %s", selected.Name))
		if _, err := api.Send(edit); err != nil {
			log.Printf("HandleCallback: error editing message: %v", err)
		}
		log.Printf("HandleCallback: saved home location %q (%s) for user %d", selected.ID, selected.Name, userID)
		return
	}

	if action == "workjp" {
		h.mu.RLock()
		matches := h.pendingWork[userID]
		h.mu.RUnlock()

		if selectionIndex < 0 || selectionIndex >= len(matches) {
			h.sendMessage(api, callback.Message.Chat.ID, "❌ Location not found in pending selections.")
			return
		}
		selected := matches[selectionIndex]

		if err := h.userStore.SetWork(userID, selected.ID); err != nil {
			log.Printf("HandleCallback: error setting work: %v", err)
			h.sendMessage(api, callback.Message.Chat.ID, "❌ Error saving preference.")
			return
		}

		h.mu.Lock()
		delete(h.pendingWork, userID)
		h.mu.Unlock()

		edit := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID,
			fmt.Sprintf("✅ Work set to: %s", selected.Name))
		if _, err := api.Send(edit); err != nil {
			log.Printf("HandleCallback: error editing message: %v", err)
		}
		log.Printf("HandleCallback: saved work location %q (%s) for user %d", selected.ID, selected.Name, userID)
		return
	}

	log.Printf("HandleCallback: unknown action: %s", action)
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
