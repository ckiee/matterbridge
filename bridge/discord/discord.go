package bdiscord

import (
	"bytes"
	"fmt"
	"strings"
	"sync"

	"github.com/42wim/matterbridge/bridge"
	"github.com/42wim/matterbridge/bridge/config"
	"github.com/42wim/matterbridge/bridge/helper"
	"github.com/matterbridge/discordgo"
)

const MessageLength = 1950

type Bdiscord struct {
	*bridge.Config

	c *discordgo.Session

	nick    string
	userID  string

	channelsMutex  sync.RWMutex
	channels       []*discordgo.Channel
	channelInfoMap map[string]*config.ChannelInfo

	membersMutex  sync.RWMutex
	userMemberMap map[string]*discordgo.Member
	nickMemberMap map[string]*discordgo.Member
}

func New(cfg *bridge.Config) bridge.Bridger {
	b := &Bdiscord{Config: cfg}
	b.userMemberMap = make(map[string]*discordgo.Member)
	b.nickMemberMap = make(map[string]*discordgo.Member)
	b.channelInfoMap = make(map[string]*config.ChannelInfo)

	return b
}

func (b *Bdiscord) Connect() error {
	var err error
	token := b.GetString("Token")
	b.Log.Info("Connecting")
	if !strings.HasPrefix(b.GetString("Token"), "Bot ") {
		token = "Bot " + b.GetString("Token")
	}
	// if we have a User token, remove the `Bot` prefix
	if strings.HasPrefix(b.GetString("Token"), "User ") {
		token = strings.Replace(b.GetString("Token"), "User ", "", -1)
	}

	b.c, err = discordgo.New(token)
	if err != nil {
		return err
	}
	b.Log.Info("Connection succeeded")
	b.c.AddHandler(b.messageCreate)
	b.c.AddHandler(b.messageTyping)
	b.c.AddHandler(b.memberUpdate)
	b.c.AddHandler(b.messageUpdate)
	b.c.AddHandler(b.messageDelete)
	b.c.AddHandler(b.messageDeleteBulk)
	b.c.AddHandler(b.memberAdd)
	b.c.AddHandler(b.memberRemove)
	// Add privileged intent for guild member tracking. This is needed to track nicks
	// for display names and @mention translation
	b.c.Identify.Intents = discordgo.MakeIntent(discordgo.IntentsAllWithoutPrivileged | discordgo.IntentsGuildMembers | discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages)

	err = b.c.Open()
	if err != nil {
		return err
	}
	// guilds, err := b.c.UserGuilds(100, "", "")
	// if err != nil {
	// 	return err
	// }
	userinfo, err := b.c.User("@me")
	if err != nil {
		return err
	}
	b.nick = userinfo.Username
	b.userID = userinfo.ID

	// Try and find this account's guild, and populate channels
	b.channelsMutex.Lock()

	// Getting this guild's channel could result in a permission error
	b.channels, err = b.c.UserChannels()
	if err != nil {
		return fmt.Errorf("could not get user channels: %s", err)
	}

	b.channelsMutex.Unlock()

	if b.GetInt("debuglevel") > 0 {
		b.Log.Debug("enabling even more discord debug")
		b.c.Debug = true
	}

	return nil
}

func (b *Bdiscord) Disconnect() error {
	return b.c.Close()
}

func (b *Bdiscord) JoinChannel(channel config.ChannelInfo) error {
	b.channelsMutex.Lock()
	defer b.channelsMutex.Unlock()

	b.channelInfoMap[channel.ID] = &channel
	return nil
}

func (b *Bdiscord) Send(msg config.Message) (string, error) {
	b.Log.Debugf("=> Receiving %#v", msg)

	channelID := b.getChannelID(msg.Channel)
	if channelID == "" {
		return "", fmt.Errorf("Could not find channelID for %v", msg.Channel)
	}

	if msg.Event == config.EventUserTyping {
		if b.GetBool("ShowUserTyping") {
			err := b.c.ChannelTyping(channelID)
			return "", err
		}
		return "", nil
	}

	// Make a action /me of the message
	if msg.Event == config.EventUserAction {
		msg.Text = "_" + msg.Text + "_"
	}

	// Handle prefix hint for unthreaded messages.
	if msg.ParentNotFound() {
		msg.ParentID = ""
		msg.Text = fmt.Sprintf("[thread]: %s", msg.Text)
	}

	// Use webhook to send the message
	// useWebhooks := b.shouldMessageUseWebhooks(&msg)
	// if useWebhooks && msg.Event != config.EventMsgDelete && msg.ParentID == "" {
	// 	return b.handleEventWebhook(&msg, channelID)
	// }

	return b.handleEventBotUser(&msg, channelID)
}

// handleEventDirect handles events via the bot user
func (b *Bdiscord) handleEventBotUser(msg *config.Message, channelID string) (string, error) {
	b.Log.Debugf("Broadcasting using token (API)")

	// Delete message
	if msg.Event == config.EventMsgDelete {
		if msg.ID == "" {
			return "", nil
		}
		err := b.c.ChannelMessageDelete(channelID, msg.ID)
		return "", err
	}

	// Upload a file if it exists
	if msg.Extra != nil {
		for _, rmsg := range helper.HandleExtra(msg, b.General) {
			rmsg.Text = helper.ClipMessage(rmsg.Text, MessageLength, b.GetString("MessageClipped"))
			if _, err := b.c.ChannelMessageSend(channelID, rmsg.Username+rmsg.Text); err != nil {
				b.Log.Errorf("Could not send message %#v: %s", rmsg, err)
			}
		}
		// check if we have files to upload (from slack, telegram or mattermost)
		if len(msg.Extra["file"]) > 0 {
			return b.handleUploadFile(msg, channelID)
		}
	}

	msg.Text = helper.ClipMessage(msg.Text, MessageLength, b.GetString("MessageClipped"))
	msg.Text = b.replaceUserMentions(msg.Text)

	// Edit message
	if msg.ID != "" {
		_, err := b.c.ChannelMessageEdit(channelID, msg.ID, msg.Username+msg.Text)
		return msg.ID, err
	}

	m := discordgo.MessageSend{
		Content:         msg.Username + msg.Text,
		AllowedMentions: b.getAllowedMentions(),
	}

	if msg.ParentValid() {
		m.Reference = &discordgo.MessageReference{
			MessageID: msg.ParentID,
			ChannelID: channelID,
			GuildID:   "",
		}
	}

	// Post normal message
	res, err := b.c.ChannelMessageSendComplex(channelID, &m)
	if err != nil {
		return "", err
	}

	return res.ID, nil
}

// handleUploadFile handles native upload of files
func (b *Bdiscord) handleUploadFile(msg *config.Message, channelID string) (string, error) {
	var err error
	for _, f := range msg.Extra["file"] {
		fi := f.(config.FileInfo)
		file := discordgo.File{
			Name:        fi.Name,
			ContentType: "",
			Reader:      bytes.NewReader(*fi.Data),
		}
		m := discordgo.MessageSend{
			Content:         msg.Username + fi.Comment,
			Files:           []*discordgo.File{&file},
			AllowedMentions: b.getAllowedMentions(),
		}
		_, err = b.c.ChannelMessageSendComplex(channelID, &m)
		if err != nil {
			return "", fmt.Errorf("file upload failed: %s", err)
		}
	}
	return "", nil
}
