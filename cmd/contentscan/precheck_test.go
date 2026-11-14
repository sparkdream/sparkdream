package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"sparkdream/tools/contentscan"
	"sparkdream/tools/contentscan/vision"
)

// The pre-check gives the verdict the worker's detector would give posted
// bytes, to callers holding the token only, and never calls text, SVG or
// media it cannot decode "clean".
func TestPrecheckHandler(t *testing.T) {
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join("..", "..", "tools", "contentscan", "vision", "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	classifier, err := vision.New(read("tiny-vit.safetensors"))
	if err != nil {
		t.Fatal(err)
	}
	picture := read("picture.png")
	const token = "zenith-aurora-zenith-aurora-zenith-aurora"
	det := contentscan.Detector{
		Classifier: classifier,
		Ruleset:    contentscan.Ruleset{Version: "test", Models: map[string]string{vision.ModelName: classifier.ModelHash()}},
	}
	call := func(h http.Handler, method, auth string, body []byte) (int, precheckResult) {
		t.Helper()
		req := httptest.NewRequest(method, precheckPath, bytes.NewReader(body))
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		var res precheckResult
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("response %q: %v", rec.Body.String(), err)
		}
		return rec.Code, res
	}
	h := precheckHandler(det, token, 1<<20)

	code, res := call(h, http.MethodPost, "Bearer "+token, picture)
	if code != http.StatusOK || res.Verdict != contentscan.VerdictClean || res.SHA256 != contentscan.SHA256Hex(picture) {
		t.Fatalf("picture: %d %+v", code, res)
	}
	if res.Models[vision.ModelName] != classifier.ModelHash() {
		t.Errorf("models = %v", res.Models)
	}

	for name, auth := range map[string]string{"no token": "", "wrong token": "Bearer nope", "token without scheme prefix of another": "Bearer " + token + "x"} {
		if code, res := call(h, http.MethodPost, auth, picture); code != http.StatusUnauthorized || res.Verdict != "" {
			t.Errorf("%s: %d %+v", name, code, res)
		}
	}
	if code, _ := call(h, http.MethodGet, "Bearer "+token, nil); code != http.StatusMethodNotAllowed {
		t.Errorf("GET: %d", code)
	}
	if code, _ := call(h, http.MethodPost, "Bearer "+token, nil); code != http.StatusBadRequest {
		t.Errorf("empty body: %d", code)
	}

	refused := map[string][]byte{
		"text":  []byte("a dream about nothing in particular"),
		"svg":   []byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect width="8" height="8"/></svg>`),
		"video": []byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom"),
	}
	for name, body := range refused {
		if code, res := call(h, http.MethodPost, "Bearer "+token, body); code != http.StatusUnprocessableEntity || res.Verdict != "" || res.Error == "" {
			t.Errorf("%s: %d %+v, want 422 with no verdict", name, code, res)
		}
	}

	// A body over the cap is refused before anything is judged.
	if code, res := call(precheckHandler(det, token, 16), http.MethodPost, "Bearer "+token, picture); code != http.StatusRequestEntityTooLarge || res.Verdict != "" {
		t.Errorf("oversized: %d %+v", code, res)
	}

	// Scores over a threshold hold the image, and a listed hash removes it.
	strict := det
	strict.Ruleset.Thresholds = map[string]float64{contentscan.CategorySexual: 0}
	if code, res := call(precheckHandler(strict, token, 1<<20), http.MethodPost, "Bearer "+token, picture); code != http.StatusOK || res.Verdict != contentscan.VerdictHeld {
		t.Errorf("zero threshold: %d %+v, want held", code, res)
	}
	listed := det
	listed.Takedown = contentscan.NewHashList(map[string]string{contentscan.SHA256Hex(picture): contentscan.CategoryIllegal})
	if code, res := call(precheckHandler(listed, token, 1<<20), http.MethodPost, "Bearer "+token, picture); code != http.StatusOK || res.Verdict != contentscan.VerdictRemoved {
		t.Errorf("listed: %d %+v, want removed", code, res)
	}

	// Without a classifier nothing is called clean.
	if code, res := call(precheckHandler(contentscan.Detector{}, token, 1<<20), http.MethodPost, "Bearer "+token, picture); code != http.StatusServiceUnavailable || res.Verdict != "" {
		t.Errorf("no classifier: %d %+v", code, res)
	}
}
