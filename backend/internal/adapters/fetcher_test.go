package adapters

import "testing"

func TestLooksBlockedDoesNotMatchGenericRobotWord(t *testing.T) {
	body := []byte(`<html><body><script>window.robotConfig = {}</script></body></html>`)

	if looksBlocked(body) {
		t.Fatal("looksBlocked() matched generic robot word")
	}
}

func TestLooksBlockedMatchesCaptchaPage(t *testing.T) {
	body := []byte(`<html><body>Подтвердите, что вы не робот</body></html>`)

	if !looksBlocked(body) {
		t.Fatal("looksBlocked() did not match captcha page")
	}
}
