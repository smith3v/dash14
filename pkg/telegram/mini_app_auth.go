package telegram

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

const launchDataLifetime = 12 * time.Hour

func telegramMiniAppUser(token, initData string, now time.Time) (int64, bool) {
	if initData == "" || len(initData) > 16<<10 {
		return 0, false
	}
	fields, err := url.ParseQuery(initData)
	if err != nil {
		return 0, false
	}
	hash, err := hex.DecodeString(fields.Get("hash"))
	if err != nil || len(hash) != sha256.Size {
		return 0, false
	}
	lines := make([]string, 0, len(fields)-1)
	for key, values := range fields {
		if key == "" || strings.ContainsAny(key, "=\r\n") || len(values) != 1 ||
			strings.ContainsAny(values[0], "\r\n") {
			return 0, false
		}
		if key != "hash" {
			lines = append(lines, key+"="+values[0])
		}
	}
	slices.Sort(lines)
	key := hmac.New(sha256.New, []byte("WebAppData"))
	key.Write([]byte(token))
	mac := hmac.New(sha256.New, key.Sum(nil))
	mac.Write([]byte(strings.Join(lines, "\n")))
	if !hmac.Equal(hash, mac.Sum(nil)) {
		return 0, false
	}

	timestamp, err := strconv.ParseInt(fields.Get("auth_date"), 10, 64)
	if err != nil {
		return 0, false
	}
	age := now.Sub(time.Unix(timestamp, 0))
	if age > launchDataLifetime || age < -time.Minute {
		return 0, false
	}
	var user struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(fields.Get("user")), &user); err != nil || user.ID <= 0 {
		return 0, false
	}
	return user.ID, true
}
