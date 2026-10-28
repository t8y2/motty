// Command i18n-catalog exports the complete source catalogs for translation.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"motty/internal/i18n"
)

func main() {
	output := flag.String("out", "", "output JSON file (default stdout)")
	flag.Parse()
	data, err := json.MarshalIndent(map[string]i18n.Messages{"en": i18n.For(i18n.English), "zh-CN": i18n.For(i18n.Chinese)}, "", "  ")
	if err == nil {
		data = append(data, '\n')
		if *output == "" {
			_, err = os.Stdout.Write(data)
		} else {
			err = os.WriteFile(*output, data, 0644)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
