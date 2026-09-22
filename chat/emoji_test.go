package chat

import (
	"fmt"
	"testing"

	"github.com/shoenig/test/must"
)

func TestEmojiResolver(t *testing.T) {
	t.Parallel()

	t.Run("fromSlack", func(t *testing.T) {
		t.Parallel()

		t.Run("should convert Slack emoji to normalized EmojiValue", func(t *testing.T) {
			t.Parallel()
			resolver := NewEmojiResolver(nil)
			must.Eq(t, "thumbs_up", resolver.FromSlack("+1").Name)
			must.Eq(t, "thumbs_up", resolver.FromSlack("thumbsup").Name)
			must.Eq(t, "thumbs_down", resolver.FromSlack("-1").Name)
			must.Eq(t, "heart", resolver.FromSlack("heart").Name)
			must.Eq(t, "fire", resolver.FromSlack("fire").Name)
		})

		t.Run("should handle colons around emoji names", func(t *testing.T) {
			t.Parallel()
			resolver := NewEmojiResolver(nil)
			must.Eq(t, "thumbs_up", resolver.FromSlack(":+1:").Name)
			must.Eq(t, "fire", resolver.FromSlack(":fire:").Name)
		})

		t.Run("should be case-insensitive", func(t *testing.T) {
			t.Parallel()
			resolver := NewEmojiResolver(nil)
			must.Eq(t, "fire", resolver.FromSlack("FIRE").Name)
			must.Eq(t, "heart", resolver.FromSlack("Heart").Name)
		})

		t.Run("should return EmojiValue with raw name if no mapping exists", func(t *testing.T) {
			t.Parallel()
			resolver := NewEmojiResolver(nil)
			result := resolver.FromSlack("custom_emoji")
			must.Eq(t, "custom_emoji", result.Name)
			must.Eq(t, "{{emoji:custom_emoji}}", result.String())
		})
	})

	t.Run("fromGChat", func(t *testing.T) {
		t.Parallel()

		t.Run("should convert GChat unicode emoji to normalized EmojiValue", func(t *testing.T) {
			t.Parallel()
			resolver := NewEmojiResolver(nil)
			must.Eq(t, "thumbs_up", resolver.FromGChat("👍").Name)
			must.Eq(t, "thumbs_down", resolver.FromGChat("👎").Name)
			must.Eq(t, "heart", resolver.FromGChat("❤️").Name)
			must.Eq(t, "fire", resolver.FromGChat("🔥").Name)
			must.Eq(t, "rocket", resolver.FromGChat("🚀").Name)
		})

		t.Run("should handle multiple unicode variants", func(t *testing.T) {
			t.Parallel()
			resolver := NewEmojiResolver(nil)
			must.Eq(t, "heart", resolver.FromGChat("❤").Name)
			must.Eq(t, "heart", resolver.FromGChat("❤️").Name)
			must.Eq(t, "check", resolver.FromGChat("✅").Name)
			must.Eq(t, "check", resolver.FromGChat("✔️").Name)
		})

		t.Run("should return EmojiValue with raw emoji as name if no mapping exists", func(t *testing.T) {
			t.Parallel()
			resolver := NewEmojiResolver(nil)
			result := resolver.FromGChat("🦄")
			must.Eq(t, "🦄", result.Name)
			must.Eq(t, "{{emoji:🦄}}", result.String())
		})
	})

	t.Run("fromTeams", func(t *testing.T) {
		t.Parallel()

		t.Run("should convert Teams reaction types to normalized EmojiValue", func(t *testing.T) {
			t.Parallel()
			resolver := NewEmojiResolver(nil)
			must.Eq(t, "thumbs_up", resolver.FromTeams("like").Name)
			must.Eq(t, "heart", resolver.FromTeams("heart").Name)
			must.Eq(t, "laugh", resolver.FromTeams("laugh").Name)
			must.Eq(t, "surprised", resolver.FromTeams("surprised").Name)
			must.Eq(t, "sad", resolver.FromTeams("sad").Name)
			must.Eq(t, "angry", resolver.FromTeams("angry").Name)
		})

		t.Run("should return EmojiValue with raw name if no mapping exists", func(t *testing.T) {
			t.Parallel()
			resolver := NewEmojiResolver(nil)
			result := resolver.FromTeams("custom_reaction")
			must.Eq(t, "custom_reaction", result.Name)
		})
	})

	t.Run("toSlack", func(t *testing.T) {
		t.Parallel()

		t.Run("should convert normalized emoji to Slack format", func(t *testing.T) {
			t.Parallel()
			resolver := NewEmojiResolver(nil)
			must.Eq(t, "+1", resolver.ToSlack("thumbs_up"))
			must.Eq(t, "fire", resolver.ToSlack("fire"))
			must.Eq(t, "heart", resolver.ToSlack("heart"))
		})

		t.Run("should return raw emoji if no mapping exists", func(t *testing.T) {
			t.Parallel()
			resolver := NewEmojiResolver(nil)
			must.Eq(t, "custom", resolver.ToSlack("custom"))
		})
	})

	t.Run("toGChat", func(t *testing.T) {
		t.Parallel()

		t.Run("should convert normalized emoji to GChat format", func(t *testing.T) {
			t.Parallel()
			resolver := NewEmojiResolver(nil)
			must.Eq(t, "👍", resolver.ToGChat("thumbs_up"))
			must.Eq(t, "🔥", resolver.ToGChat("fire"))
			must.Eq(t, "🚀", resolver.ToGChat("rocket"))
		})

		t.Run("should return raw emoji if no mapping exists", func(t *testing.T) {
			t.Parallel()
			resolver := NewEmojiResolver(nil)
			must.Eq(t, "custom", resolver.ToGChat("custom"))
		})
	})

	t.Run("matches", func(t *testing.T) {
		t.Parallel()

		t.Run("should match Slack format to normalized emoji", func(t *testing.T) {
			t.Parallel()
			resolver := NewEmojiResolver(nil)
			must.True(t, resolver.Matches("+1", "thumbs_up"))
			must.True(t, resolver.Matches("thumbsup", "thumbs_up"))
			must.True(t, resolver.Matches(":+1:", "thumbs_up"))
			must.True(t, resolver.Matches("fire", "fire"))
		})

		t.Run("should match GChat format to normalized emoji", func(t *testing.T) {
			t.Parallel()
			resolver := NewEmojiResolver(nil)
			must.True(t, resolver.Matches("👍", "thumbs_up"))
			must.True(t, resolver.Matches("🔥", "fire"))
			must.True(t, resolver.Matches("❤️", "heart"))
		})

		t.Run("should not match different emoji", func(t *testing.T) {
			t.Parallel()
			resolver := NewEmojiResolver(nil)
			must.False(t, resolver.Matches("+1", "thumbs_down"))
			must.False(t, resolver.Matches("👍", "fire"))
		})

		t.Run("should match unmapped emoji by equality", func(t *testing.T) {
			t.Parallel()
			resolver := NewEmojiResolver(nil)
			must.True(t, resolver.Matches("custom", "custom"))
			must.False(t, resolver.Matches("custom", "other"))
		})
	})

	t.Run("extend", func(t *testing.T) {
		t.Parallel()

		t.Run("should add new emoji mappings", func(t *testing.T) {
			t.Parallel()
			resolver := NewEmojiResolver(nil)
			resolver.Extend(CustomEmojiMap{
				"unicorn": {Slack: []string{"unicorn_face"}, GChat: []string{"🦄"}},
			})

			must.Eq(t, "unicorn", resolver.FromSlack("unicorn_face").Name)
			must.Eq(t, "unicorn", resolver.FromGChat("🦄").Name)
			must.Eq(t, "unicorn_face", resolver.ToSlack("unicorn"))
			must.Eq(t, "🦄", resolver.ToGChat("unicorn"))
		})

		t.Run("should override existing mappings", func(t *testing.T) {
			t.Parallel()
			resolver := NewEmojiResolver(nil)
			resolver.Extend(CustomEmojiMap{
				"fire": {Slack: []string{"flames"}, GChat: []string{"🔥"}},
			})

			must.Eq(t, "fire", resolver.FromSlack("flames").Name)
			must.Eq(t, "flames", resolver.ToSlack("fire"))
		})
	})

	t.Run("defaultEmojiResolver", func(t *testing.T) {
		t.Parallel()

		t.Run("should be a pre-configured resolver instance", func(t *testing.T) {
			t.Parallel()
			must.Eq(t, "thumbs_up", DefaultEmojiResolver.FromSlack("+1").Name)
		})
	})

	t.Run("DEFAULT_EMOJI_MAP", func(t *testing.T) {
		t.Parallel()

		t.Run("should contain all well-known emoji", func(t *testing.T) {
			t.Parallel()
			expectedEmoji := []string{
				// Reactions & Gestures
				"thumbs_up",
				"thumbs_down",
				"clap",
				"wave",
				"pray",
				"muscle",
				"ok_hand",
				"point_up",
				"point_down",
				"point_left",
				"point_right",
				"raised_hands",
				"shrug",
				"facepalm",
				// Emotions & Faces
				"heart",
				"smile",
				"laugh",
				"thinking",
				"sad",
				"cry",
				"angry",
				"love_eyes",
				"cool",
				"wink",
				"surprised",
				"worried",
				"confused",
				"neutral",
				"sleeping",
				"sick",
				"mind_blown",
				"relieved",
				"grimace",
				"rolling_eyes",
				"hug",
				"zany",
				// Status & Symbols
				"check",
				"x",
				"question",
				"exclamation",
				"warning",
				"stop",
				"info",
				"100",
				"fire",
				"star",
				"sparkles",
				"lightning",
				"boom",
				"eyes",
				// Status Indicators
				"green_circle",
				"yellow_circle",
				"red_circle",
				"blue_circle",
				"white_circle",
				"black_circle",
				// Objects & Tools
				"rocket",
				"party",
				"confetti",
				"balloon",
				"gift",
				"trophy",
				"medal",
				"lightbulb",
				"gear",
				"wrench",
				"hammer",
				"bug",
				"link",
				"lock",
				"unlock",
				"key",
				"pin",
				"memo",
				"clipboard",
				"calendar",
				"clock",
				"hourglass",
				"bell",
				"megaphone",
				"speech_bubble",
				"email",
				"inbox",
				"outbox",
				"package",
				"folder",
				"file",
				"chart_up",
				"chart_down",
				"coffee",
				"pizza",
				"beer",
				// Arrows & Directions
				"arrow_up",
				"arrow_down",
				"arrow_left",
				"arrow_right",
				"refresh",
				// Nature & Weather
				"sun",
				"cloud",
				"rain",
				"snow",
				"rainbow",
			}

			for _, e := range expectedEmoji {
				formats, ok := DefaultEmojiMap[e]
				must.True(t, ok)
				must.True(t, len(formats.Slack) > 0)
				must.True(t, len(formats.GChat) > 0)
			}
		})
	})
}

func TestEmojiHelper(t *testing.T) {
	t.Parallel()

	t.Run("should provide EmojiValue objects for well-known emoji", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "thumbs_up", Emoji.Get("thumbs_up").Name)
		must.Eq(t, "fire", Emoji.Get("fire").Name)
		must.Eq(t, "rocket", Emoji.Get("rocket").Name)
		must.Eq(t, "100", Emoji.Get("100").Name)
	})

	t.Run("should convert to placeholder string via toString()", func(t *testing.T) {
		t.Parallel()
		must.Eq(t, "{{emoji:thumbs_up}}", Emoji.Get("thumbs_up").String())
		must.Eq(t, "{{emoji:fire}}", Emoji.Get("fire").String())
		must.Eq(t, "{{emoji:rocket}}", fmt.Sprint(Emoji.Get("rocket")))
	})

	t.Run("should have object identity (same emoji returns same object)", func(t *testing.T) {
		t.Parallel()
		// TS === identity ports as EmojiValue equality.
		must.Eq(t, Emoji.Get("thumbs_up"), Emoji.Get("thumbs_up"))
		must.Eq(t, Emoji.Get("fire"), Emoji.Get("fire"))
		must.Eq(t, GetEmoji("thumbs_up"), Emoji.Get("thumbs_up"))
	})

	t.Run("should have a custom() method that returns EmojiValue", func(t *testing.T) {
		t.Parallel()
		unicorn := Emoji.Custom("unicorn")
		must.Eq(t, "unicorn", unicorn.Name)
		must.Eq(t, "{{emoji:unicorn}}", unicorn.String())

		custom := Emoji.Custom("custom_team_emoji")
		must.Eq(t, "custom_team_emoji", custom.Name)
		must.Eq(t, "{{emoji:custom_team_emoji}}", fmt.Sprint(custom))
	})

	t.Run("should return same object from custom() for same name", func(t *testing.T) {
		t.Parallel()
		first := Emoji.Custom("test_emoji")
		second := Emoji.Custom("test_emoji")
		must.Eq(t, first, second)
	})
}

func TestConvertEmojiPlaceholders(t *testing.T) {
	t.Parallel()

	t.Run("should convert placeholders to Slack format", func(t *testing.T) {
		t.Parallel()
		text := fmt.Sprintf("Thanks! %s Great work! %s", Emoji.Get("thumbs_up"), Emoji.Get("fire"))
		result := ConvertEmojiPlaceholders(text, "slack")
		must.Eq(t, "Thanks! :+1: Great work! :fire:", result)
	})

	t.Run("should convert placeholders to GChat format", func(t *testing.T) {
		t.Parallel()
		text := fmt.Sprintf("Thanks! %s Great work! %s", Emoji.Get("thumbs_up"), Emoji.Get("fire"))
		result := ConvertEmojiPlaceholders(text, "gchat")
		must.Eq(t, "Thanks! 👍 Great work! 🔥", result)
	})

	t.Run("should convert placeholders to Teams format (unicode)", func(t *testing.T) {
		t.Parallel()
		text := fmt.Sprintf("Thanks! %s Great work! %s", Emoji.Get("thumbs_up"), Emoji.Get("fire"))
		result := ConvertEmojiPlaceholders(text, "teams")
		must.Eq(t, "Thanks! 👍 Great work! 🔥", result)
	})

	t.Run("should handle unknown emoji by passing through", func(t *testing.T) {
		t.Parallel()
		text := "Check this {{emoji:unknown_emoji}}!"
		result := ConvertEmojiPlaceholders(text, "slack")
		must.Eq(t, "Check this :unknown_emoji:!", result)
	})

	t.Run("should handle multiple emoji in a message", func(t *testing.T) {
		t.Parallel()
		text := fmt.Sprintf("%s Hello! %s How are you? %s", Emoji.Get("wave"), Emoji.Get("smile"), Emoji.Get("thumbs_up"))
		result := ConvertEmojiPlaceholders(text, "gchat")
		must.Eq(t, "👋 Hello! 😊 How are you? 👍", result)
	})

	t.Run("should handle text with no emoji", func(t *testing.T) {
		t.Parallel()
		text := "Just a regular message"
		result := ConvertEmojiPlaceholders(text, "slack")
		must.Eq(t, "Just a regular message", result)
	})

	t.Run("should convert placeholders to Messenger format (unicode)", func(t *testing.T) {
		t.Parallel()
		text := fmt.Sprintf("Thanks! %s Great work! %s", Emoji.Get("thumbs_up"), Emoji.Get("fire"))
		result := ConvertEmojiPlaceholders(text, "messenger")
		must.Eq(t, "Thanks! 👍 Great work! 🔥", result)
	})

	t.Run("should convert placeholders to Notion format (unicode)", func(t *testing.T) {
		t.Parallel()
		text := fmt.Sprintf("Thanks! %s Great work! %s", Emoji.Get("thumbs_up"), Emoji.Get("wave"))
		result := ConvertEmojiPlaceholders(text, "notion")
		must.Eq(t, "Thanks! 👍 Great work! 👋", result)
	})

	t.Run("should convert multiple Messenger emoji in a message", func(t *testing.T) {
		t.Parallel()
		text := fmt.Sprintf("%s Hello! %s How are you? %s", Emoji.Get("wave"), Emoji.Get("smile"), Emoji.Get("rocket"))
		result := ConvertEmojiPlaceholders(text, "messenger")
		must.Eq(t, "👋 Hello! 😊 How are you? 🚀", result)
	})

	t.Run("should pass through unknown emoji for Messenger", func(t *testing.T) {
		t.Parallel()
		text := "Check this {{emoji:unknown_emoji}}!"
		result := ConvertEmojiPlaceholders(text, "messenger")
		must.Eq(t, "Check this unknown_emoji!", result)
	})

	t.Run("should handle Messenger emoji with no placeholders", func(t *testing.T) {
		t.Parallel()
		text := "Plain message with no emoji"
		result := ConvertEmojiPlaceholders(text, "messenger")
		must.Eq(t, "Plain message with no emoji", result)
	})

	t.Run("should produce identical output for Messenger and other unicode platforms", func(t *testing.T) {
		t.Parallel()
		text := fmt.Sprintf("%s %s %s %s", Emoji.Get("heart"), Emoji.Get("check"), Emoji.Get("star"), Emoji.Get("party"))
		messenger := ConvertEmojiPlaceholders(text, "messenger")
		gchat := ConvertEmojiPlaceholders(text, "gchat")
		must.Eq(t, messenger, gchat)
	})
}

func TestCreateEmoji(t *testing.T) {
	t.Parallel()

	t.Run("should create emoji helper with well-known EmojiValue objects", func(t *testing.T) {
		t.Parallel()
		e := CreateEmoji(nil)
		must.Eq(t, "thumbs_up", e.Get("thumbs_up").Name)
		must.Eq(t, "fire", e.Get("fire").Name)
		must.Eq(t, "rocket", e.Get("rocket").Name)
		must.Eq(t, "{{emoji:thumbs_up}}", fmt.Sprint(e.Get("thumbs_up")))
	})

	t.Run("should include custom() method returning EmojiValue", func(t *testing.T) {
		t.Parallel()
		e := CreateEmoji(nil)
		unicorn := e.Custom("unicorn")
		must.Eq(t, "unicorn", unicorn.Name)
		must.Eq(t, "{{emoji:unicorn}}", unicorn.String())
	})

	t.Run("should add custom emoji to the helper as EmojiValue objects", func(t *testing.T) {
		t.Parallel()
		e := CreateEmoji(CustomEmojiMap{
			"unicorn":      {Slack: []string{"unicorn_face"}, GChat: []string{"🦄"}},
			"company_logo": {Slack: []string{"company"}, GChat: []string{"🏢"}},
		})

		must.Eq(t, "unicorn", e.Get("unicorn").Name)
		must.Eq(t, "company_logo", e.Get("company_logo").Name)
		must.Eq(t, "{{emoji:unicorn}}", fmt.Sprint(e.Get("unicorn")))
		must.Eq(t, "{{emoji:company_logo}}", fmt.Sprint(e.Get("company_logo")))

		must.Eq(t, "thumbs_up", e.Get("thumbs_up").Name)
	})

	t.Run("should automatically register custom emoji with default resolver", func(t *testing.T) {
		t.Parallel()
		e := CreateEmoji(CustomEmojiMap{
			"custom_test": {Slack: []string{"custom_slack"}, GChat: []string{"🎯"}},
		})

		text := fmt.Sprintf("%s Magic!", e.Get("custom_test"))
		must.Eq(t, ":custom_slack: Magic!", ConvertEmojiPlaceholders(text, "slack"))
		must.Eq(t, "🎯 Magic!", ConvertEmojiPlaceholders(text, "gchat"))
	})

	t.Run("should return same EmojiValue singleton as emoji helper", func(t *testing.T) {
		t.Parallel()
		e := CreateEmoji(nil)
		must.Eq(t, e.Get("thumbs_up"), Emoji.Get("thumbs_up"))
		must.Eq(t, e.Get("fire"), Emoji.Get("fire"))
	})
}
