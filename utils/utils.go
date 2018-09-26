package utils

import "net/url"

func IsValidRemoteURL(toTest string) bool {
	uri, err := url.ParseRequestURI(toTest)
	hostname := uri.Hostname()
	if err != nil || hostname == "" {
		return false
	}
	return true
}
