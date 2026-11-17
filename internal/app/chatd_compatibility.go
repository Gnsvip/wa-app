package app

import (
	"regexp"
	"strings"
)

// Connections announce a supported build independently of the APK material used
// for registration. Keep registration's version and token material together.
const defaultChatdAppVersion = "2.26.40.76"

var chatdClientOutdatedPattern = regexp.MustCompile(`\bserver sent failure\b[^\n<]*\breason=405(?:\s|$)`)

func chatdAppVersion(version string) string {
	version = strings.TrimSpace(version)
	current, minimum := parseVersion(version), parseVersion(defaultChatdAppVersion)
	for i := range current {
		if current[i] > minimum[i] {
			return version
		}
		if current[i] < minimum[i] {
			return defaultChatdAppVersion
		}
	}
	return version
}

func isChatdClientOutdated(err error) bool {
	return err != nil && chatdClientOutdatedPattern.MatchString(err.Error())
}
