package llm

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

var pngHeader = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

func imageRequest() Request {
	return Request{Model: "m", Messages: []Message{
		{Role: RoleUser, Text: "what is this", Images: []Image{{MediaType: ImagePNG, Data: pngHeader}}},
	}}
}

func TestDetectImageReadsTheSignature(t *testing.T) {
	cases := map[string][]byte{
		ImagePNG:  pngHeader,
		ImageJPEG: []byte("\xff\xd8\xff\xe0\x00\x10JFIF"),
		ImageGIF:  []byte("GIF89a\x01\x00"),
		ImageWebP: []byte("RIFF\x24\x00\x00\x00WEBPVP8 "),
		"":        []byte("<svg xmlns='http://www.w3.org/2000/svg'/>"),
	}
	for want, data := range cases {
		if got := DetectImage(data); got != want {
			t.Errorf("DetectImage(%q) = %q, want %q", data[:4], got, want)
		}
	}
}

func TestAnthropicSendsImagesBeforeTheText(t *testing.T) {
	body, err := (&anthropic{}).buildBody(imageRequest())
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Messages []struct {
			Content []struct {
				Type   string `json:"type"`
				Text   string `json:"text"`
				Source struct {
					Type, MediaType, Data string
				} `json:"source"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	blocks := got.Messages[0].Content
	if len(blocks) != 2 || blocks[0].Type != "image" || blocks[1].Type != "text" || blocks[1].Text != "what is this" {
		t.Fatalf("blocks = %+v", blocks)
	}
	if !strings.Contains(string(body), `"media_type":"image/png"`) || !strings.Contains(string(body), base64.StdEncoding.EncodeToString(pngHeader)) {
		t.Fatalf("image source missing from %s", body)
	}
}

func TestOpenAIChatSendsAPictureAsADataURL(t *testing.T) {
	body, err := (&openAI{}).buildBody(imageRequest())
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Messages []struct {
			Content []struct {
				Type     string `json:"type"`
				Text     string `json:"text"`
				ImageURL struct {
					URL string `json:"url"`
				} `json:"image_url"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	parts := got.Messages[0].Content
	want := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngHeader)
	if len(parts) != 2 || parts[0].Type != "text" || parts[0].Text != "what is this" || parts[1].Type != "image_url" || parts[1].ImageURL.URL != want {
		t.Fatalf("parts = %+v", parts)
	}
}

func TestOpenAIChatKeepsPlainTextAString(t *testing.T) {
	body, err := (&openAI{}).buildBody(Request{Model: "m", Messages: []Message{{Role: RoleUser, Text: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"content":"hi"`) {
		t.Fatalf("body = %s", body)
	}
}

func TestResponsesSendsAPictureAsInputImage(t *testing.T) {
	body, err := (&openAIResponses{}).buildBody(imageRequest())
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Input []struct {
			Content []struct {
				Type     string `json:"type"`
				Text     string `json:"text"`
				ImageURL string `json:"image_url"`
			} `json:"content"`
		} `json:"input"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	parts := got.Input[0].Content
	want := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngHeader)
	if len(parts) != 2 || parts[0].Type != "input_text" || parts[0].Text != "what is this" || parts[1].Type != "input_image" || parts[1].ImageURL != want {
		t.Fatalf("parts = %+v", parts)
	}
}

func TestRequestRejectsAnImageThatIsNotWhatItSays(t *testing.T) {
	bad := imageRequest()
	bad.Messages[0].Images[0].MediaType = ImageJPEG
	if _, err := (&anthropic{}).buildBody(bad); err == nil {
		t.Fatal("a PNG declared as JPEG was accepted")
	}
	assistant := imageRequest()
	assistant.Messages[0].Role = RoleAssistant
	if _, err := (&openAI{}).buildBody(assistant); err == nil {
		t.Fatal("an assistant message carried an image")
	}
}

func TestMessageJSONLeavesImagesOut(t *testing.T) {
	raw, err := json.Marshal(imageRequest().Messages)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "Images") || strings.Contains(string(raw), "image/png") {
		t.Fatalf("history JSON carries the image: %s", raw)
	}
}
