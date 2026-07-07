package auth

import (
	"fmt"
	"strings"
	"unicode"
)

const maxSessionLength = 64

type ParsedUsername struct {
	Base    string
	Region  string
	Session string
}

func ParseUsername(raw string) (ParsedUsername, error) {
	if raw == "" {
		return ParsedUsername{}, fmt.Errorf("username is empty")
	}

	regionStart := strings.Index(raw, "-region-")
	sessionStart := strings.Index(raw, "-session-")
	baseEnd := firstMarker(regionStart, sessionStart, len(raw))
	if baseEnd == 0 {
		return ParsedUsername{}, fmt.Errorf("base username is empty")
	}

	parsed := ParsedUsername{Base: raw[:baseEnd]}
	remainder := raw[baseEnd:]
	if remainder == "" {
		return parsed, nil
	}

	if strings.HasPrefix(remainder, "-region-") {
		region, rest, err := parseRegion(remainder[len("-region-"):])
		if err != nil {
			return ParsedUsername{}, err
		}
		parsed.Region = region
		remainder = rest
	}

	if strings.HasPrefix(remainder, "-session-") {
		session, rest, err := parseSession(remainder[len("-session-"):])
		if err != nil {
			return ParsedUsername{}, err
		}
		parsed.Session = session
		remainder = rest
	}

	if remainder != "" {
		return ParsedUsername{}, fmt.Errorf("invalid username DSL suffix: %s", remainder)
	}

	return parsed, nil
}

func firstMarker(regionStart int, sessionStart int, defaultEnd int) int {
	if regionStart < 0 && sessionStart < 0 {
		return defaultEnd
	}
	if regionStart < 0 {
		return sessionStart
	}
	if sessionStart < 0 || regionStart < sessionStart {
		return regionStart
	}
	return sessionStart
}

func parseRegion(raw string) (string, string, error) {
	value, rest := splitValue(raw)
	if len(value) != 2 || !isAlpha(value) {
		return "", "", fmt.Errorf("region must be 2 alpha characters")
	}
	return strings.ToLower(value), rest, nil
}

func parseSession(raw string) (string, string, error) {
	value, rest := splitValue(raw)
	if value == "" {
		return "", "", fmt.Errorf("session is empty")
	}
	if len(value) > maxSessionLength {
		return "", "", fmt.Errorf("session exceeds %d characters", maxSessionLength)
	}
	if !isSessionID(value) {
		return "", "", fmt.Errorf("session contains invalid characters")
	}
	return value, rest, nil
}

func splitValue(raw string) (string, string) {
	regionStart := strings.Index(raw, "-region-")
	sessionStart := strings.Index(raw, "-session-")
	valueEnd := firstMarker(regionStart, sessionStart, len(raw))
	return raw[:valueEnd], raw[valueEnd:]
}

func isAlpha(value string) bool {
	for _, r := range value {
		if !unicode.IsLetter(r) || r > unicode.MaxASCII {
			return false
		}
	}
	return true
}

func isSessionID(value string) bool {
	for _, r := range value {
		if r == '-' || r == '_' || unicode.IsDigit(r) || isASCIIAlpha(r) {
			continue
		}
		return false
	}
	return true
}

func isASCIIAlpha(r rune) bool {
	return r <= unicode.MaxASCII && unicode.IsLetter(r)
}
