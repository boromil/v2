// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package dsui // import "miniflux.app/v2/internal/dsui"

import (
	"fmt"
	"net/http"

	"miniflux.app/v2/internal/config"
	"miniflux.app/v2/internal/http/request"
	"miniflux.app/v2/internal/http/response"
	"miniflux.app/v2/internal/locale"
	"miniflux.app/v2/internal/model"
	"miniflux.app/v2/internal/ui/form"
)

// integrationFieldView is one input of an integration block.
type integrationFieldView struct {
	Name     string
	Label    string
	Type     string // text, url, password, number, select, checkbox
	Value    string
	Checked  bool
	ReadOnly bool
	Options  []optionView
}

type optionView struct {
	Value    string
	Label    string
	Selected bool
}

// integrationSectionView is one integration of the settings page.
type integrationSectionView struct {
	Title      string
	EnableName string
	Enabled    bool
	Fields     []integrationFieldView
	Hint       string
}

func textField(name, label, value string) integrationFieldView {
	return integrationFieldView{Name: name, Label: label, Type: "text", Value: value}
}

func urlField(name, label, value string) integrationFieldView {
	return integrationFieldView{Name: name, Label: label, Type: "url", Value: value}
}

func secretField(name, label, value string) integrationFieldView {
	return integrationFieldView{Name: name, Label: label, Type: "password", Value: value}
}

func numberField(name, label string, value int) integrationFieldView {
	return integrationFieldView{Name: name, Label: label, Type: "number", Value: fmt.Sprintf("%d", value)}
}

func optionalNumberField(name, label string, value *int64) integrationFieldView {
	text := ""
	if value != nil {
		text = fmt.Sprintf("%d", *value)
	}
	return integrationFieldView{Name: name, Label: label, Type: "number", Value: text}
}

func checkboxField(name, label string, checked bool) integrationFieldView {
	return integrationFieldView{Name: name, Label: label, Type: "checkbox", Checked: checked}
}

// integrationSections lists every integration with its fields. Adding a new
// integration means adding one entry here; the form itself is generic.
func integrationSections(integration *model.Integration, printer *locale.Printer) []integrationSectionView {
	print := printer.Print

	return []integrationSectionView{
		{
			Title: print("form.integration.archiveorg_activate"), EnableName: "archiveorg_enabled", Enabled: integration.ArchiveorgEnabled,
		},
		{
			Title: print("form.integration.apprise_activate"), EnableName: "apprise_enabled", Enabled: integration.AppriseEnabled,
			Fields: []integrationFieldView{
				urlField("apprise_url", print("form.integration.apprise_url"), integration.AppriseURL),
				textField("apprise_services_url", print("form.integration.apprise_services_url"), integration.AppriseServicesURL),
			},
		},
		{
			Title: print("form.integration.betula_activate"), EnableName: "betula_enabled", Enabled: integration.BetulaEnabled,
			Fields: []integrationFieldView{
				urlField("betula_url", print("form.integration.betula_url"), integration.BetulaURL),
				textField("betula_token", print("form.integration.betula_token"), integration.BetulaToken),
			},
		},
		{
			Title: print("form.integration.cubox_activate"), EnableName: "cubox_enabled", Enabled: integration.CuboxEnabled,
			Fields: []integrationFieldView{
				urlField("cubox_api_link", print("form.integration.cubox_api_link"), integration.CuboxAPILink),
			},
		},
		{
			Title: print("form.integration.discord_activate"), EnableName: "discord_enabled", Enabled: integration.DiscordEnabled,
			Fields: []integrationFieldView{
				urlField("discord_webhook_link", print("form.integration.discord_webhook_link"), integration.DiscordWebhookLink),
			},
		},
		{
			Title: print("form.integration.espial_activate"), EnableName: "espial_enabled", Enabled: integration.EspialEnabled,
			Fields: []integrationFieldView{
				urlField("espial_url", print("form.integration.espial_endpoint"), integration.EspialURL),
				textField("espial_api_key", print("form.integration.espial_api_key"), integration.EspialAPIKey),
				textField("espial_tags", print("form.integration.espial_tags"), integration.EspialTags),
			},
		},
		{
			Title: print("form.integration.fever_activate"), EnableName: "fever_enabled", Enabled: integration.FeverEnabled,
			Fields: []integrationFieldView{
				textField("fever_username", print("form.integration.fever_username"), integration.FeverUsername),
				secretField("fever_password", print("form.integration.fever_password"), ""),
			},
			Hint: print("form.integration.fever_endpoint") + " " + config.Opts.BaseURL() + "/fever/",
		},
		{
			Title: print("form.integration.googlereader_activate"), EnableName: "googlereader_enabled", Enabled: integration.GoogleReaderEnabled,
			Fields: []integrationFieldView{
				textField("googlereader_username", print("form.integration.googlereader_username"), integration.GoogleReaderUsername),
				secretField("googlereader_password", print("form.integration.googlereader_password"), ""),
			},
			Hint: print("form.integration.googlereader_endpoint") + " " + config.Opts.BaseURL() + "/",
		},
		{
			Title: print("form.integration.gotify_activate"), EnableName: "gotify_enabled", Enabled: integration.GotifyEnabled,
			Fields: []integrationFieldView{
				urlField("gotify_url", print("form.integration.gotify_server_url"), integration.GotifyURL),
				secretField("gotify_token", print("form.integration.gotify_token"), integration.GotifyToken),
				numberField("gotify_priority", print("form.integration.gotify_priority"), integration.GotifyPriority),
			},
		},
		{
			Title: print("form.integration.instapaper_activate"), EnableName: "instapaper_enabled", Enabled: integration.InstapaperEnabled,
			Fields: []integrationFieldView{
				textField("instapaper_username", print("form.integration.instapaper_username"), integration.InstapaperUsername),
				secretField("instapaper_password", print("form.integration.instapaper_password"), integration.InstapaperPassword),
			},
		},
		{
			Title: print("form.integration.karakeep_activate"), EnableName: "karakeep_enabled", Enabled: integration.KarakeepEnabled,
			Fields: []integrationFieldView{
				textField("karakeep_api_key", print("form.integration.karakeep_api_key"), integration.KarakeepAPIKey),
				urlField("karakeep_url", print("form.integration.karakeep_url"), integration.KarakeepURL),
				textField("karakeep_tags", print("form.integration.karakeep_tags"), integration.KarakeepTags),
			},
		},
		{
			Title: print("form.integration.linkace_activate"), EnableName: "linkace_enabled", Enabled: integration.LinkAceEnabled,
			Fields: []integrationFieldView{
				urlField("linkace_url", print("form.integration.linkace_endpoint"), integration.LinkAceURL),
				textField("linkace_api_key", print("form.integration.linkace_api_key"), integration.LinkAceAPIKey),
				textField("linkace_tags", print("form.integration.linkace_tags"), integration.LinkAceTags),
				checkboxField("linkace_is_private", print("form.integration.linkace_is_private"), integration.LinkAcePrivate),
				checkboxField("linkace_check_disabled", print("form.integration.linkace_check_disabled"), integration.LinkAceCheckDisabled),
			},
		},
		{
			Title: print("form.integration.linkding_activate"), EnableName: "linkding_enabled", Enabled: integration.LinkdingEnabled,
			Fields: []integrationFieldView{
				urlField("linkding_url", print("form.integration.linkding_endpoint"), integration.LinkdingURL),
				textField("linkding_api_key", print("form.integration.linkding_api_key"), integration.LinkdingAPIKey),
				textField("linkding_tags", print("form.integration.linkding_tags"), integration.LinkdingTags),
				checkboxField("linkding_mark_as_unread", print("form.integration.linkding_bookmark"), integration.LinkdingMarkAsUnread),
			},
		},
		{
			Title: print("form.integration.linktaco_activate"), EnableName: "linktaco_enabled", Enabled: integration.LinktacoEnabled,
			Fields: []integrationFieldView{
				secretField("linktaco_api_token", print("form.integration.linktaco_api_token"), integration.LinktacoAPIToken),
				textField("linktaco_org_slug", print("form.integration.linktaco_org_slug"), integration.LinktacoOrgSlug),
				textField("linktaco_tags", print("form.integration.linktaco_tags"), integration.LinktacoTags),
				{
					Name:  "linktaco_visibility",
					Label: print("form.integration.linktaco_visibility"),
					Type:  "select",
					Options: []optionView{
						{Value: "PUBLIC", Label: print("form.integration.linktaco_visibility_public"), Selected: integration.LinktacoVisibility != "PRIVATE"},
						{Value: "PRIVATE", Label: print("form.integration.linktaco_visibility_private"), Selected: integration.LinktacoVisibility == "PRIVATE"},
					},
				},
			},
			Hint: print("form.integration.linktaco_visibility_hint"),
		},
		{
			Title: print("form.integration.linkwarden_activate"), EnableName: "linkwarden_enabled", Enabled: integration.LinkwardenEnabled,
			Fields: []integrationFieldView{
				urlField("linkwarden_url", print("form.integration.linkwarden_endpoint"), integration.LinkwardenURL),
				textField("linkwarden_api_key", print("form.integration.linkwarden_api_key"), integration.LinkwardenAPIKey),
				optionalNumberField("linkwarden_collection_id", print("form.integration.linkwarden_collection_id"), integration.LinkwardenCollectionID),
			},
		},
		{
			Title: print("form.integration.matrix_bot_activate"), EnableName: "matrix_bot_enabled", Enabled: integration.MatrixBotEnabled,
			Fields: []integrationFieldView{
				textField("matrix_bot_user", print("form.integration.matrix_bot_user"), integration.MatrixBotUser),
				secretField("matrix_bot_password", print("form.integration.matrix_bot_password"), integration.MatrixBotPassword),
				urlField("matrix_bot_url", print("form.integration.matrix_bot_url"), integration.MatrixBotURL),
				textField("matrix_bot_chat_id", print("form.integration.matrix_bot_chat_id"), integration.MatrixBotChatID),
			},
		},
		{
			Title: print("form.integration.notion_activate"), EnableName: "notion_enabled", Enabled: integration.NotionEnabled,
			Fields: []integrationFieldView{
				secretField("notion_token", print("form.integration.notion_token"), integration.NotionToken),
				textField("notion_page_id", print("form.integration.notion_page_id"), integration.NotionPageID),
			},
		},
		{
			Title: print("form.integration.ntfy_activate"), EnableName: "ntfy_enabled", Enabled: integration.NtfyEnabled,
			Fields: []integrationFieldView{
				textField("ntfy_topic", print("form.integration.ntfy_topic"), integration.NtfyTopic),
				urlField("ntfy_url", print("form.integration.ntfy_url"), integration.NtfyURL),
				textField("ntfy_api_token", print("form.integration.ntfy_api_token"), integration.NtfyAPIToken),
				textField("ntfy_username", print("form.integration.ntfy_username"), integration.NtfyUsername),
				textField("ntfy_password", print("form.integration.ntfy_password"), integration.NtfyPassword),
				urlField("ntfy_icon_url", print("form.integration.ntfy_icon_url"), integration.NtfyIconURL),
				checkboxField("ntfy_internal_links", print("form.integration.ntfy_internal_links"), integration.NtfyInternalLinks),
			},
		},
		{
			Title: print("form.integration.nunux_keeper_activate"), EnableName: "nunux_keeper_enabled", Enabled: integration.NunuxKeeperEnabled,
			Fields: []integrationFieldView{
				urlField("nunux_keeper_url", print("form.integration.nunux_keeper_endpoint"), integration.NunuxKeeperURL),
				textField("nunux_keeper_api_key", print("form.integration.nunux_keeper_api_key"), integration.NunuxKeeperAPIKey),
			},
		},
		{
			Title: print("form.integration.omnivore_activate"), EnableName: "omnivore_enabled", Enabled: integration.OmnivoreEnabled,
			Fields: []integrationFieldView{
				textField("omnivore_api_key", print("form.integration.omnivore_api_key"), integration.OmnivoreAPIKey),
				urlField("omnivore_url", print("form.integration.omnivore_url"), integration.OmnivoreURL),
			},
		},
		{
			Title: print("form.integration.pinboard_activate"), EnableName: "pinboard_enabled", Enabled: integration.PinboardEnabled,
			Fields: []integrationFieldView{
				secretField("pinboard_token", print("form.integration.pinboard_token"), integration.PinboardToken),
				textField("pinboard_tags", print("form.integration.pinboard_tags"), integration.PinboardTags),
				checkboxField("pinboard_mark_as_unread", print("form.integration.pinboard_bookmark"), integration.PinboardMarkAsUnread),
			},
		},
		{
			Title: print("form.integration.pushover_activate"), EnableName: "pushover_enabled", Enabled: integration.PushoverEnabled,
			Fields: []integrationFieldView{
				textField("pushover_token", print("form.integration.pushover_token"), integration.PushoverToken),
				textField("pushover_user", print("form.integration.pushover_user"), integration.PushoverUser),
				textField("pushover_device", print("form.integration.pushover_device"), integration.PushoverDevice),
				textField("pushover_prefix", print("form.integration.pushover_prefix"), integration.PushoverPrefix),
			},
		},
		{
			Title: print("form.integration.raindrop_activate"), EnableName: "raindrop_enabled", Enabled: integration.RaindropEnabled,
			Fields: []integrationFieldView{
				textField("raindrop_token", print("form.integration.raindrop_token"), integration.RaindropToken),
				textField("raindrop_collection_id", print("form.integration.raindrop_collection_id"), integration.RaindropCollectionID),
				textField("raindrop_tags", print("form.integration.raindrop_tags"), integration.RaindropTags),
			},
		},
		{
			Title: print("form.integration.readeck_activate"), EnableName: "readeck_enabled", Enabled: integration.ReadeckEnabled,
			Fields: []integrationFieldView{
				checkboxField("readeck_push_enabled", print("form.integration.readeck_push_activate"), integration.ReadeckPushEnabled),
				checkboxField("readeck_only_url", print("form.integration.readeck_only_url"), integration.ReadeckOnlyURL),
				urlField("readeck_url", print("form.integration.readeck_endpoint"), integration.ReadeckURL),
				textField("readeck_api_key", print("form.integration.readeck_api_key"), integration.ReadeckAPIKey),
				textField("readeck_labels", print("form.integration.readeck_labels"), integration.ReadeckLabels),
			},
		},
		{
			Title: print("form.integration.readwise_activate"), EnableName: "readwise_enabled", Enabled: integration.ReadwiseEnabled,
			Fields: []integrationFieldView{
				textField("readwise_api_key", print("form.integration.readwise_api_key"), integration.ReadwiseAPIKey),
			},
		},
		{
			Title: print("form.integration.rssbridge_activate"), EnableName: "rssbridge_enabled", Enabled: integration.RSSBridgeEnabled,
			Fields: []integrationFieldView{
				urlField("rssbridge_url", print("form.integration.rssbridge_url"), integration.RSSBridgeURL),
				secretField("rssbridge_token", print("form.integration.rssbridge_token"), integration.RSSBridgeToken),
			},
		},
		{
			Title: print("form.integration.shaarli_activate"), EnableName: "shaarli_enabled", Enabled: integration.ShaarliEnabled,
			Fields: []integrationFieldView{
				urlField("shaarli_url", print("form.integration.shaarli_endpoint"), integration.ShaarliURL),
				secretField("shaarli_api_secret", print("form.integration.shaarli_api_secret"), integration.ShaarliAPISecret),
			},
		},
		{
			Title: print("form.integration.shiori_activate"), EnableName: "shiori_enabled", Enabled: integration.ShioriEnabled,
			Fields: []integrationFieldView{
				urlField("shiori_url", print("form.integration.shiori_endpoint"), integration.ShioriURL),
				textField("shiori_username", print("form.integration.shiori_username"), integration.ShioriUsername),
				secretField("shiori_password", print("form.integration.shiori_password"), integration.ShioriPassword),
			},
		},
		{
			Title: print("form.integration.slack_activate"), EnableName: "slack_enabled", Enabled: integration.SlackEnabled,
			Fields: []integrationFieldView{
				urlField("slack_webhook_link", print("form.integration.slack_webhook_link"), integration.SlackWebhookLink),
			},
		},
		{
			Title: print("form.integration.telegram_bot_activate"), EnableName: "telegram_bot_enabled", Enabled: integration.TelegramBotEnabled,
			Fields: []integrationFieldView{
				textField("telegram_bot_token", print("form.integration.telegram_bot_token"), integration.TelegramBotToken),
				textField("telegram_bot_chat_id", print("form.integration.telegram_chat_id"), integration.TelegramBotChatID),
				optionalNumberField("telegram_bot_topic_id", print("form.integration.telegram_topic_id"), integration.TelegramBotTopicID),
				checkboxField("telegram_bot_disable_web_page_preview", print("form.integration.telegram_bot_disable_web_page_preview"), integration.TelegramBotDisableWebPagePreview),
				checkboxField("telegram_bot_disable_notification", print("form.integration.telegram_bot_disable_notification"), integration.TelegramBotDisableNotification),
				checkboxField("telegram_bot_disable_buttons", print("form.integration.telegram_bot_disable_buttons"), integration.TelegramBotDisableButtons),
			},
		},
		{
			Title: print("form.integration.wallabag_activate"), EnableName: "wallabag_enabled", Enabled: integration.WallabagEnabled,
			Fields: []integrationFieldView{
				checkboxField("wallabag_only_url", print("form.integration.wallabag_only_url"), integration.WallabagOnlyURL),
				urlField("wallabag_url", print("form.integration.wallabag_endpoint"), integration.WallabagURL),
				textField("wallabag_client_id", print("form.integration.wallabag_client_id"), integration.WallabagClientID),
				secretField("wallabag_client_secret", print("form.integration.wallabag_client_secret"), integration.WallabagClientSecret),
				textField("wallabag_username", print("form.integration.wallabag_username"), integration.WallabagUsername),
				secretField("wallabag_password", print("form.integration.wallabag_password"), integration.WallabagPassword),
				textField("wallabag_tags", print("form.integration.wallabag_tags"), integration.WallabagTags),
			},
		},
		{
			Title: print("form.integration.webhook_activate"), EnableName: "webhook_enabled", Enabled: integration.WebhookEnabled,
			Fields: []integrationFieldView{
				urlField("webhook_url", print("form.integration.webhook_url"), integration.WebhookURL),
				{
					Name:     "webhook_secret",
					Label:    print("form.integration.webhook_secret"),
					Type:     "text",
					Value:    integration.WebhookSecret,
					ReadOnly: true,
				},
			},
		},
	}
}

// showIntegrations renders the integrations page.
func (h *handler) showIntegrations(w http.ResponseWriter, r *http.Request) {
	user, err := h.store.UserByID(request.UserID(r))
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	integration, err := h.store.Integration(user.ID)
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	flashError, flashSuccess, clearFlash := takeFlash(r)
	if clearFlash != nil {
		http.SetCookie(w, clearFlash)
	}

	vm := pageViewModel{
		appViewModel: h.newPageViewModel(r, user),
		PageTemplate: "integrations",
		PageHeading:  printerFor(user.Language).Print("page.integrations.title"),
		FlashError:   flashError,
		FlashSuccess: flashSuccess,
		Integrations: integrationSections(integration, printerFor(user.Language)),
	}
	vm.Title = vm.PageHeading + " — Miniflux"

	h.renderPage(w, r, vm)
}

// saveIntegrations persists the integration form and redirects back to the page.
func (h *handler) saveIntegrations(w http.ResponseWriter, r *http.Request) {
	user, err := h.store.UserByID(request.UserID(r))
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	printer := printerFor(user.Language)
	integrationForm := form.NewIntegrationForm(r)
	if localizedErr := integrationForm.Apply(h.store, user.ID); localizedErr != nil {
		setFlashError(w, localizedErr.Translate(user.Language))
	} else {
		setFlashSuccess(w, printer.Print("alert.prefs_saved"))
	}

	response.HTMLRedirect(w, r, "/ds/integrations")
}
