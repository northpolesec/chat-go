// Ported from packages/chat/src/emoji.ts @ 6adca36 (chat v4.40.0).
// Divergences: EmojiValue is a comparable struct; GetEmoji returns equal values
// (not pointer identity). No singleton registry (TS Map existed only for ===).
// EmojiFormats.Slack/GChat are []string (TS string | string[]). CustomEmojiMap
// is map[string]EmojiFormats (TS empty interface for module augmentation).
// WellKnownEmoji is a string type with constants. The emoji helper is
// Emoji.Get/Custom instead of dynamic object fields. DefaultEmojiResolver.Extend
// is mutex-guarded (shared mutable resolver). ConvertEmojiPlaceholders takes
// (text, platform) and always uses DefaultEmojiResolver (TS optional 3rd arg).
// Extend rebuilds reverse maps from the current emojiMap (TS incrementally
// writes and can leave stale reverse-map keys).
package chat

import (
	"encoding/json"
	"maps"
	"regexp"
	"slices"
	"strings"
	"sync"
)

// WellKnownEmoji is a normalized emoji name that ships in DefaultEmojiMap.
type WellKnownEmoji string

const (
	EmojiThumbsUp    WellKnownEmoji = "thumbs_up"
	EmojiThumbsDown  WellKnownEmoji = "thumbs_down"
	EmojiClap        WellKnownEmoji = "clap"
	EmojiWave        WellKnownEmoji = "wave"
	EmojiPray        WellKnownEmoji = "pray"
	EmojiMuscle      WellKnownEmoji = "muscle"
	EmojiOKHand      WellKnownEmoji = "ok_hand"
	EmojiPointUp     WellKnownEmoji = "point_up"
	EmojiPointDown   WellKnownEmoji = "point_down"
	EmojiPointLeft   WellKnownEmoji = "point_left"
	EmojiPointRight  WellKnownEmoji = "point_right"
	EmojiRaisedHands WellKnownEmoji = "raised_hands"
	EmojiShrug       WellKnownEmoji = "shrug"
	EmojiFacepalm    WellKnownEmoji = "facepalm"

	EmojiHeart       WellKnownEmoji = "heart"
	EmojiSmile       WellKnownEmoji = "smile"
	EmojiLaugh       WellKnownEmoji = "laugh"
	EmojiThinking    WellKnownEmoji = "thinking"
	EmojiSad         WellKnownEmoji = "sad"
	EmojiCry         WellKnownEmoji = "cry"
	EmojiAngry       WellKnownEmoji = "angry"
	EmojiLoveEyes    WellKnownEmoji = "love_eyes"
	EmojiCool        WellKnownEmoji = "cool"
	EmojiWink        WellKnownEmoji = "wink"
	EmojiSurprised   WellKnownEmoji = "surprised"
	EmojiWorried     WellKnownEmoji = "worried"
	EmojiConfused    WellKnownEmoji = "confused"
	EmojiNeutral     WellKnownEmoji = "neutral"
	EmojiSleeping    WellKnownEmoji = "sleeping"
	EmojiSick        WellKnownEmoji = "sick"
	EmojiMindBlown   WellKnownEmoji = "mind_blown"
	EmojiRelieved    WellKnownEmoji = "relieved"
	EmojiGrimace     WellKnownEmoji = "grimace"
	EmojiRollingEyes WellKnownEmoji = "rolling_eyes"
	EmojiHug         WellKnownEmoji = "hug"
	EmojiZany        WellKnownEmoji = "zany"

	EmojiCheck       WellKnownEmoji = "check"
	EmojiX           WellKnownEmoji = "x"
	EmojiQuestion    WellKnownEmoji = "question"
	EmojiExclamation WellKnownEmoji = "exclamation"
	EmojiWarning     WellKnownEmoji = "warning"
	EmojiStop        WellKnownEmoji = "stop"
	EmojiInfo        WellKnownEmoji = "info"
	Emoji100         WellKnownEmoji = "100"
	EmojiFire        WellKnownEmoji = "fire"
	EmojiStar        WellKnownEmoji = "star"
	EmojiSparkles    WellKnownEmoji = "sparkles"
	EmojiLightning   WellKnownEmoji = "lightning"
	EmojiBoom        WellKnownEmoji = "boom"
	EmojiEyes        WellKnownEmoji = "eyes"

	EmojiGreenCircle  WellKnownEmoji = "green_circle"
	EmojiYellowCircle WellKnownEmoji = "yellow_circle"
	EmojiRedCircle    WellKnownEmoji = "red_circle"
	EmojiBlueCircle   WellKnownEmoji = "blue_circle"
	EmojiWhiteCircle  WellKnownEmoji = "white_circle"
	EmojiBlackCircle  WellKnownEmoji = "black_circle"

	EmojiRocket       WellKnownEmoji = "rocket"
	EmojiParty        WellKnownEmoji = "party"
	EmojiConfetti     WellKnownEmoji = "confetti"
	EmojiBalloon      WellKnownEmoji = "balloon"
	EmojiGift         WellKnownEmoji = "gift"
	EmojiTrophy       WellKnownEmoji = "trophy"
	EmojiMedal        WellKnownEmoji = "medal"
	EmojiLightbulb    WellKnownEmoji = "lightbulb"
	EmojiGear         WellKnownEmoji = "gear"
	EmojiWrench       WellKnownEmoji = "wrench"
	EmojiHammer       WellKnownEmoji = "hammer"
	EmojiBug          WellKnownEmoji = "bug"
	EmojiLink         WellKnownEmoji = "link"
	EmojiLock         WellKnownEmoji = "lock"
	EmojiUnlock       WellKnownEmoji = "unlock"
	EmojiKey          WellKnownEmoji = "key"
	EmojiPin          WellKnownEmoji = "pin"
	EmojiMemo         WellKnownEmoji = "memo"
	EmojiClipboard    WellKnownEmoji = "clipboard"
	EmojiCalendar     WellKnownEmoji = "calendar"
	EmojiClock        WellKnownEmoji = "clock"
	EmojiHourglass    WellKnownEmoji = "hourglass"
	EmojiBell         WellKnownEmoji = "bell"
	EmojiMegaphone    WellKnownEmoji = "megaphone"
	EmojiSpeechBubble WellKnownEmoji = "speech_bubble"
	EmojiEmail        WellKnownEmoji = "email"
	EmojiInbox        WellKnownEmoji = "inbox"
	EmojiOutbox       WellKnownEmoji = "outbox"
	EmojiPackage      WellKnownEmoji = "package"
	EmojiFolder       WellKnownEmoji = "folder"
	EmojiFile         WellKnownEmoji = "file"
	EmojiChartUp      WellKnownEmoji = "chart_up"
	EmojiChartDown    WellKnownEmoji = "chart_down"
	EmojiCoffee       WellKnownEmoji = "coffee"
	EmojiPizza        WellKnownEmoji = "pizza"
	EmojiBeer         WellKnownEmoji = "beer"

	EmojiArrowUp    WellKnownEmoji = "arrow_up"
	EmojiArrowDown  WellKnownEmoji = "arrow_down"
	EmojiArrowLeft  WellKnownEmoji = "arrow_left"
	EmojiArrowRight WellKnownEmoji = "arrow_right"
	EmojiRefresh    WellKnownEmoji = "refresh"

	EmojiSun     WellKnownEmoji = "sun"
	EmojiCloud   WellKnownEmoji = "cloud"
	EmojiRain    WellKnownEmoji = "rain"
	EmojiSnow    WellKnownEmoji = "snow"
	EmojiRainbow WellKnownEmoji = "rainbow"
)

// EmojiFormats is the Slack and Google Chat spellings of one normalized emoji.
type EmojiFormats struct {
	GChat []string
	Slack []string
}

// CustomEmojiMap is a user-supplied name→formats table.
type CustomEmojiMap map[string]EmojiFormats

// GetEmoji returns an EmojiValue for name. Same name → equal value.
func GetEmoji(name string) EmojiValue {
	return EmojiValue{Name: name}
}

func (e EmojiValue) String() string {
	return "{{emoji:" + e.Name + "}}"
}

func (e EmojiValue) MarshalJSON() ([]byte, error) {
	return json.Marshal(e.String())
}

// DefaultEmojiMap maps normalized names to platform formats.
// Read-only: do not mutate; register extras via CreateEmoji or EmojiResolver.Extend.
var DefaultEmojiMap = map[string]EmojiFormats{
	"thumbs_up":    {Slack: []string{"+1", "thumbsup"}, GChat: []string{"👍"}},
	"thumbs_down":  {Slack: []string{"-1", "thumbsdown"}, GChat: []string{"👎"}},
	"clap":         {Slack: []string{"clap"}, GChat: []string{"👏"}},
	"wave":         {Slack: []string{"wave"}, GChat: []string{"👋"}},
	"pray":         {Slack: []string{"pray"}, GChat: []string{"🙏"}},
	"muscle":       {Slack: []string{"muscle"}, GChat: []string{"💪"}},
	"ok_hand":      {Slack: []string{"ok_hand"}, GChat: []string{"👌"}},
	"point_up":     {Slack: []string{"point_up"}, GChat: []string{"👆"}},
	"point_down":   {Slack: []string{"point_down"}, GChat: []string{"👇"}},
	"point_left":   {Slack: []string{"point_left"}, GChat: []string{"👈"}},
	"point_right":  {Slack: []string{"point_right"}, GChat: []string{"👉"}},
	"raised_hands": {Slack: []string{"raised_hands"}, GChat: []string{"🙌"}},
	"shrug":        {Slack: []string{"shrug"}, GChat: []string{"🤷"}},
	"facepalm":     {Slack: []string{"facepalm"}, GChat: []string{"🤦"}},

	"heart":        {Slack: []string{"heart"}, GChat: []string{"❤️", "❤"}},
	"smile":        {Slack: []string{"smile", "slightly_smiling_face"}, GChat: []string{"😊"}},
	"laugh":        {Slack: []string{"laughing", "satisfied", "joy"}, GChat: []string{"😂", "😆"}},
	"thinking":     {Slack: []string{"thinking_face"}, GChat: []string{"🤔"}},
	"sad":          {Slack: []string{"cry", "sad", "white_frowning_face"}, GChat: []string{"😢"}},
	"cry":          {Slack: []string{"sob"}, GChat: []string{"😭"}},
	"angry":        {Slack: []string{"angry"}, GChat: []string{"😠"}},
	"love_eyes":    {Slack: []string{"heart_eyes"}, GChat: []string{"😍"}},
	"cool":         {Slack: []string{"sunglasses"}, GChat: []string{"😎"}},
	"wink":         {Slack: []string{"wink"}, GChat: []string{"😉"}},
	"surprised":    {Slack: []string{"open_mouth"}, GChat: []string{"😮"}},
	"worried":      {Slack: []string{"worried"}, GChat: []string{"😟"}},
	"confused":     {Slack: []string{"confused"}, GChat: []string{"😕"}},
	"neutral":      {Slack: []string{"neutral_face"}, GChat: []string{"😐"}},
	"sleeping":     {Slack: []string{"sleeping"}, GChat: []string{"😴"}},
	"sick":         {Slack: []string{"nauseated_face"}, GChat: []string{"🤢"}},
	"mind_blown":   {Slack: []string{"exploding_head"}, GChat: []string{"🤯"}},
	"relieved":     {Slack: []string{"relieved"}, GChat: []string{"😌"}},
	"grimace":      {Slack: []string{"grimacing"}, GChat: []string{"😬"}},
	"rolling_eyes": {Slack: []string{"rolling_eyes"}, GChat: []string{"🙄"}},
	"hug":          {Slack: []string{"hugging_face"}, GChat: []string{"🤗"}},
	"zany":         {Slack: []string{"zany_face"}, GChat: []string{"🤪"}},

	"check":       {Slack: []string{"white_check_mark", "heavy_check_mark"}, GChat: []string{"✅", "✔️"}},
	"x":           {Slack: []string{"x", "heavy_multiplication_x"}, GChat: []string{"❌", "✖️"}},
	"question":    {Slack: []string{"question"}, GChat: []string{"❓", "?"}},
	"exclamation": {Slack: []string{"exclamation"}, GChat: []string{"❗"}},
	"warning":     {Slack: []string{"warning"}, GChat: []string{"⚠️"}},
	"stop":        {Slack: []string{"octagonal_sign"}, GChat: []string{"🛑"}},
	"info":        {Slack: []string{"information_source"}, GChat: []string{"ℹ️"}},
	"100":         {Slack: []string{"100"}, GChat: []string{"💯"}},
	"fire":        {Slack: []string{"fire"}, GChat: []string{"🔥"}},
	"star":        {Slack: []string{"star"}, GChat: []string{"⭐"}},
	"sparkles":    {Slack: []string{"sparkles"}, GChat: []string{"✨"}},
	"lightning":   {Slack: []string{"zap"}, GChat: []string{"⚡"}},
	"boom":        {Slack: []string{"boom"}, GChat: []string{"💥"}},
	"eyes":        {Slack: []string{"eyes"}, GChat: []string{"👀"}},

	"green_circle":  {Slack: []string{"large_green_circle"}, GChat: []string{"🟢"}},
	"yellow_circle": {Slack: []string{"large_yellow_circle"}, GChat: []string{"🟡"}},
	"red_circle":    {Slack: []string{"red_circle"}, GChat: []string{"🔴"}},
	"blue_circle":   {Slack: []string{"large_blue_circle"}, GChat: []string{"🔵"}},
	"white_circle":  {Slack: []string{"white_circle"}, GChat: []string{"⚪"}},
	"black_circle":  {Slack: []string{"black_circle"}, GChat: []string{"⚫"}},

	"rocket":        {Slack: []string{"rocket"}, GChat: []string{"🚀"}},
	"party":         {Slack: []string{"tada", "partying_face"}, GChat: []string{"🎉", "🥳"}},
	"confetti":      {Slack: []string{"confetti_ball"}, GChat: []string{"🎊"}},
	"balloon":       {Slack: []string{"balloon"}, GChat: []string{"🎈"}},
	"gift":          {Slack: []string{"gift"}, GChat: []string{"🎁"}},
	"trophy":        {Slack: []string{"trophy"}, GChat: []string{"🏆"}},
	"medal":         {Slack: []string{"first_place_medal"}, GChat: []string{"🥇"}},
	"lightbulb":     {Slack: []string{"bulb"}, GChat: []string{"💡"}},
	"gear":          {Slack: []string{"gear"}, GChat: []string{"⚙️"}},
	"wrench":        {Slack: []string{"wrench"}, GChat: []string{"🔧"}},
	"hammer":        {Slack: []string{"hammer"}, GChat: []string{"🔨"}},
	"bug":           {Slack: []string{"bug"}, GChat: []string{"🐛"}},
	"link":          {Slack: []string{"link"}, GChat: []string{"🔗"}},
	"lock":          {Slack: []string{"lock"}, GChat: []string{"🔒"}},
	"unlock":        {Slack: []string{"unlock"}, GChat: []string{"🔓"}},
	"key":           {Slack: []string{"key"}, GChat: []string{"🔑"}},
	"pin":           {Slack: []string{"pushpin"}, GChat: []string{"📌"}},
	"memo":          {Slack: []string{"memo"}, GChat: []string{"📝"}},
	"clipboard":     {Slack: []string{"clipboard"}, GChat: []string{"📋"}},
	"calendar":      {Slack: []string{"calendar"}, GChat: []string{"📅"}},
	"clock":         {Slack: []string{"clock1"}, GChat: []string{"🕐"}},
	"hourglass":     {Slack: []string{"hourglass"}, GChat: []string{"⏳"}},
	"bell":          {Slack: []string{"bell"}, GChat: []string{"🔔"}},
	"megaphone":     {Slack: []string{"mega"}, GChat: []string{"📢"}},
	"speech_bubble": {Slack: []string{"speech_balloon"}, GChat: []string{"💬"}},
	"email":         {Slack: []string{"email"}, GChat: []string{"📧"}},
	"inbox":         {Slack: []string{"inbox_tray"}, GChat: []string{"📥"}},
	"outbox":        {Slack: []string{"outbox_tray"}, GChat: []string{"📤"}},
	"package":       {Slack: []string{"package"}, GChat: []string{"📦"}},
	"folder":        {Slack: []string{"file_folder"}, GChat: []string{"📁"}},
	"file":          {Slack: []string{"page_facing_up"}, GChat: []string{"📄"}},
	"chart_up":      {Slack: []string{"chart_with_upwards_trend"}, GChat: []string{"📈"}},
	"chart_down":    {Slack: []string{"chart_with_downwards_trend"}, GChat: []string{"📉"}},
	"coffee":        {Slack: []string{"coffee"}, GChat: []string{"☕"}},
	"pizza":         {Slack: []string{"pizza"}, GChat: []string{"🍕"}},
	"beer":          {Slack: []string{"beer"}, GChat: []string{"🍺"}},

	"arrow_up":    {Slack: []string{"arrow_up"}, GChat: []string{"⬆️"}},
	"arrow_down":  {Slack: []string{"arrow_down"}, GChat: []string{"⬇️"}},
	"arrow_left":  {Slack: []string{"arrow_left"}, GChat: []string{"⬅️"}},
	"arrow_right": {Slack: []string{"arrow_right"}, GChat: []string{"➡️"}},
	"refresh":     {Slack: []string{"arrows_counterclockwise"}, GChat: []string{"🔄"}},

	"sun":     {Slack: []string{"sunny"}, GChat: []string{"☀️"}},
	"cloud":   {Slack: []string{"cloud"}, GChat: []string{"☁️"}},
	"rain":    {Slack: []string{"rain_cloud"}, GChat: []string{"🌧️"}},
	"snow":    {Slack: []string{"snowflake"}, GChat: []string{"❄️"}},
	"rainbow": {Slack: []string{"rainbow"}, GChat: []string{"🌈"}},
}

var teamsToNormalized = map[string]string{
	"like":      "thumbs_up",
	"heart":     "heart",
	"laugh":     "laugh",
	"surprised": "surprised",
	"sad":       "sad",
	"angry":     "angry",
}

var emojiPlaceholderRE = regexp.MustCompile(`(?i)\{\{emoji:([a-z0-9_]+)\}\}`)

// EmojiResolver converts between platform formats and normalized names.
type EmojiResolver struct {
	mu                sync.RWMutex
	emojiMap          map[string]EmojiFormats
	slackToNormalized map[string]string
	gchatToNormalized map[string]string
}

// NewEmojiResolver builds a resolver from DefaultEmojiMap plus optional custom entries.
func NewEmojiResolver(custom CustomEmojiMap) *EmojiResolver {
	r := &EmojiResolver{
		emojiMap:          mergeEmojiMaps(DefaultEmojiMap, custom),
		slackToNormalized: map[string]string{},
		gchatToNormalized: map[string]string{},
	}
	r.buildReverseMaps()
	return r
}

func mergeEmojiMaps(base, custom map[string]EmojiFormats) map[string]EmojiFormats {
	out := make(map[string]EmojiFormats, len(base)+len(custom))
	maps.Copy(out, base)
	maps.Copy(out, custom)
	return out
}

func (r *EmojiResolver) buildReverseMaps() {
	r.slackToNormalized = make(map[string]string, len(r.emojiMap))
	r.gchatToNormalized = make(map[string]string, len(r.emojiMap))
	for normalized, formats := range r.emojiMap {
		for _, slack := range formats.Slack {
			r.slackToNormalized[strings.ToLower(slack)] = normalized
		}
		for _, gchat := range formats.GChat {
			r.gchatToNormalized[gchat] = normalized
		}
	}
}

func stripSlackColons(s string) string {
	s = strings.TrimPrefix(s, ":")
	s = strings.TrimSuffix(s, ":")
	return s
}

func firstFormat(vals []string) string {
	if len(vals) == 0 {
		return ""
	}
	return vals[0]
}

// FromSlack converts a Slack emoji name to a normalized EmojiValue.
func (r *EmojiResolver) FromSlack(slackEmoji string) EmojiValue {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cleaned := strings.ToLower(stripSlackColons(slackEmoji))
	normalized, ok := r.slackToNormalized[cleaned]
	if !ok {
		return GetEmoji(slackEmoji)
	}
	return GetEmoji(normalized)
}

// FromGChat converts a Google Chat unicode emoji to a normalized EmojiValue.
func (r *EmojiResolver) FromGChat(gchatEmoji string) EmojiValue {
	r.mu.RLock()
	defer r.mu.RUnlock()
	normalized, ok := r.gchatToNormalized[gchatEmoji]
	if !ok {
		return GetEmoji(gchatEmoji)
	}
	return GetEmoji(normalized)
}

// FromTeams converts a Teams reaction type to a normalized EmojiValue.
func (r *EmojiResolver) FromTeams(teamsReaction string) EmojiValue {
	normalized, ok := teamsToNormalized[teamsReaction]
	if !ok {
		return GetEmoji(teamsReaction)
	}
	return GetEmoji(normalized)
}

// ToSlack converts a normalized emoji name to the first Slack format.
func (r *EmojiResolver) ToSlack(name string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	formats, ok := r.emojiMap[name]
	if !ok {
		return name
	}
	return firstFormat(formats.Slack)
}

// ToGChat converts a normalized emoji name to the first Google Chat format.
func (r *EmojiResolver) ToGChat(name string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	formats, ok := r.emojiMap[name]
	if !ok {
		return name
	}
	return firstFormat(formats.GChat)
}

// Matches reports whether rawEmoji is a Slack or GChat spelling of normalized.
func (r *EmojiResolver) Matches(rawEmoji, normalized string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	formats, ok := r.emojiMap[normalized]
	if !ok {
		return rawEmoji == normalized
	}
	cleanedRaw := strings.ToLower(stripSlackColons(rawEmoji))
	for _, slack := range formats.Slack {
		if strings.ToLower(slack) == cleanedRaw {
			return true
		}
	}
	return slices.Contains(formats.GChat, rawEmoji)
}

// Extend adds or overrides emoji mappings.
func (r *EmojiResolver) Extend(custom CustomEmojiMap) {
	r.mu.Lock()
	defer r.mu.Unlock()
	maps.Copy(r.emojiMap, custom)
	r.buildReverseMaps()
}

// DefaultEmojiResolver is the process-wide resolver. CreateEmoji extends it.
var DefaultEmojiResolver = NewEmojiResolver(nil)

type emojiHelper struct{}

func (emojiHelper) Get(name string) EmojiValue {
	return GetEmoji(name)
}

func (emojiHelper) Custom(name string) EmojiValue {
	return GetEmoji(name)
}

// Emoji is the package-level helper (TS `emoji`).
var Emoji emojiHelper

// CreateEmoji returns a helper and registers custom entries on DefaultEmojiResolver.
func CreateEmoji(custom CustomEmojiMap) emojiHelper {
	if len(custom) > 0 {
		DefaultEmojiResolver.Extend(custom)
	}
	return emojiHelper{}
}

// ConvertEmojiPlaceholders rewrites {{emoji:name}} tokens for a platform.
func ConvertEmojiPlaceholders(text, platform string) string {
	return emojiPlaceholderRE.ReplaceAllStringFunc(text, func(match string) string {
		name := emojiPlaceholderRE.FindStringSubmatch(match)[1]
		switch platform {
		case "slack":
			return ":" + DefaultEmojiResolver.ToSlack(name) + ":"
		default:
			return DefaultEmojiResolver.ToGChat(name)
		}
	})
}
