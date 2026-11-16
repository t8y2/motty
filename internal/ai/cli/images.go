package cli

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"motty/internal/ai/llm"
	"os"
	"path/filepath"
)

// conversationImages lists the pictures of the whole conversation, oldest first.
// The conversation text the CLIs are given names each one where it was attached.
func conversationImages(messages []llm.Message) []llm.Image {
	var images []llm.Image
	for _, message := range messages {
		images = append(images, message.Images...)
	}
	return images
}

func imageExtension(mediaType string) string {
	switch mediaType {
	case llm.ImageJPEG:
		return ".jpg"
	case llm.ImageGIF:
		return ".gif"
	case llm.ImageWebP:
		return ".webp"
	}
	return ".png"
}

// writeImages saves pictures as private files in dir, which is removed when the
// run ends, for the CLIs that read them from a path.
func writeImages(dir string, images []llm.Image) ([]string, error) {
	files := make([]string, 0, len(images))
	for index, image := range images {
		if llm.DetectImage(image.Data) != image.MediaType {
			return nil, errors.New("cli: invalid_image")
		}
		file := filepath.Join(dir, fmt.Sprintf("image-%d%s", index+1, imageExtension(image.MediaType)))
		if err := os.WriteFile(file, image.Data, 0o600); err != nil {
			return nil, errors.New("cli: runtime_config_failed")
		}
		files = append(files, file)
	}
	return files, nil
}

// claudeImageInput is the one stream-json user message that carries the pictures
// and then the prompt.
func claudeImageInput(prompt string, images []llm.Image) (string, error) {
	type source struct {
		Type      string `json:"type"`
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
	}
	type block struct {
		Type   string  `json:"type"`
		Text   string  `json:"text,omitempty"`
		Source *source `json:"source,omitempty"`
	}
	blocks := make([]block, 0, len(images)+1)
	for _, image := range images {
		if llm.DetectImage(image.Data) != image.MediaType {
			return "", errors.New("cli: invalid_image")
		}
		blocks = append(blocks, block{Type: "image", Source: &source{Type: "base64", MediaType: image.MediaType, Data: base64.StdEncoding.EncodeToString(image.Data)}})
	}
	blocks = append(blocks, block{Type: "text", Text: prompt})
	line, err := json.Marshal(map[string]any{
		"type":    "user",
		"message": map[string]any{"role": "user", "content": blocks},
	})
	if err != nil {
		return "", errors.New("cli: input_limit")
	}
	return string(line) + "\n", nil
}
