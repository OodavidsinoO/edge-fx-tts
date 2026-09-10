package businessConsts

import (
	"fmt"
	"strings"
)

var (
	BaseUrl            = "speech.platform.bing.com/consumer/speech/synthesize/readaloud"
	TrustedClientToken = "6A5AA1D4EAFF4E9FB37E23D68491D6F4"

	EdgeWssEndpoint   = fmt.Sprintf("wss://%s/edge/v1?TrustedClientToken=%s", BaseUrl, TrustedClientToken)
	VoiceListEndpoint = fmt.Sprintf("https://%s/voices/list?trustedclienttoken=%s", BaseUrl, TrustedClientToken)

	ChromiumFllVersion   = "143.0.3650.75"
	ChromiumMajorVersion = strings.Split(ChromiumFllVersion, ".")[0]
)

const (
	DefaultVoice = "en-US-EmmaMultilingualNeural"
)
