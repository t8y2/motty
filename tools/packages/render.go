package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"text/template"
)

// caskTemplate is the cask the tap serves. The disk image is the notarized and
// stapled package, so Gatekeeper has a ticket to check even offline; the zip is
// not used here because a bundle that was notarized as part of an image has no
// ticket of its own. auto_updates is true because an installed copy replaces
// itself through the in-app updater; without it Homebrew would think the cask is
// outdated the moment the app updates.
var caskTemplate = template.Must(template.New("cask").Parse(`cask "motty" do
  arch arm: "arm64", intel: "amd64"

  version "{{.Version}}"
  sha256 arm:   "{{.ARM64}}",
         intel: "{{.AMD64}}"

  url "https://github.com/{{.Repository}}/releases/download/v#{version}/motty-#{version}-darwin-#{arch}.dmg"
  name "{{.Name}}"
  desc "{{.Description}}"
  homepage "{{.Homepage}}"

  auto_updates true
  depends_on macos: {{.MacOS}}

  app "Motty.app"

  zap trash: "~/Library/Application Support/motty"
end
`))

type caskData struct {
	Version     string
	Repository  string
	Name        string
	Description string
	Homepage    string
	MacOS       string
	ARM64       string
	AMD64       string
}

// renderCask is the Casks/motty.rb the tap serves.
func renderCask(version string, files downloads) string {
	var buffer bytes.Buffer
	// The template is a compile-time constant with every field present, so a
	// failure here can only be a bug in this file.
	if err := caskTemplate.Execute(&buffer, caskData{
		Version: version, Repository: repository, Name: name, Description: description,
		Homepage: homepage, MacOS: macOSSymbol,
		ARM64: files.dmgARM64.sum, AMD64: files.dmgAMD64.sum,
	}); err != nil {
		panic(fmt.Sprintf("packages: rendering the cask: %v", err))
	}
	return buffer.String()
}

// scoopManifest is the scoop-bucket manifest: the portable zip, because a
// portable copy keeps everything in motty-data (which Scoop persists across
// versions) and does not update itself, leaving that to scoop.
type scoopManifest struct {
	Version      string                  `json:"version"`
	Description  string                  `json:"description"`
	Homepage     string                  `json:"homepage"`
	License      string                  `json:"license"`
	Architecture map[string]scoopPackage `json:"architecture"`
	Shortcuts    [][]string              `json:"shortcuts"`
	Persist      string                  `json:"persist"`
	Checkver     scoopCheckver           `json:"checkver"`
	Autoupdate   scoopAutoupdate         `json:"autoupdate"`
}

type scoopPackage struct {
	URL  string `json:"url"`
	Hash string `json:"hash"`
}

type scoopCheckver struct {
	GitHub string `json:"github"`
}

type scoopAutoupdate struct {
	Architecture map[string]scoopUpdate `json:"architecture"`
	Hash         scoopHash              `json:"hash"`
}

// scoopUpdate is a package an autoupdate points at; its digest comes from the
// hash stanza rather than from the package itself.
type scoopUpdate struct {
	URL string `json:"url"`
}

// scoopHash takes the digest from the release's own SHA256SUMS instead of
// downloading the package, so an autoupdate checks the bytes against the file
// the release was built with.
type scoopHash struct {
	URL  string `json:"url"`
	Find string `json:"find"`
}

// download is one versioned package on GitHub releases.
func download(version, file string) string {
	return "https://github.com/" + repository + "/releases/download/v" + version + "/" + file
}

// renderManifest is the motty.json the bucket serves.
func renderManifest(version string, files downloads) ([]byte, error) {
	manifest := scoopManifest{
		Version:     version,
		Description: description,
		Homepage:    homepage,
		License:     license,
		Architecture: map[string]scoopPackage{
			"64bit": {URL: download(version, files.zipAMD64.name), Hash: files.zipAMD64.sum},
			"arm64": {URL: download(version, files.zipARM64.name), Hash: files.zipARM64.sum},
		},
		Shortcuts: [][]string{{"motty.exe", name}},
		Persist:   "motty-data",
		Checkver:  scoopCheckver{GitHub: "https://github.com/" + repository},
		Autoupdate: scoopAutoupdate{
			Architecture: map[string]scoopUpdate{
				"64bit": {URL: "https://github.com/" + repository + "/releases/download/v$version/motty-$version-windows-amd64.zip"},
				"arm64": {URL: "https://github.com/" + repository + "/releases/download/v$version/motty-$version-windows-arm64.zip"},
			},
			Hash: scoopHash{
				URL:  "https://github.com/" + repository + "/releases/download/v$version/SHA256SUMS",
				Find: "$basename",
			},
		},
	}
	body, err := json.MarshalIndent(manifest, "", "    ")
	if err != nil {
		return nil, err
	}
	return append(body, '\n'), nil
}
