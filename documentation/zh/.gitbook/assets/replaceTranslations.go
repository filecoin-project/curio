package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path"

	"github.com/snadrus/must"
)

type Translation struct {
	Message     string `json:"message"`
	Translation string `json:"translation"`
}

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: go run replaceTranslations.go <input_xml_file> <json_translation_file>")
		fmt.Println("Outputs: input_xml_file_translated.xml")
		return
	}

	xmlBytes := must.One(ioutil.ReadFile(os.Args[1]))
	jsonFile := must.One(ioutil.ReadFile(os.Args[2]))

	// Parse the JSON file into a struct
	var translations struct {
		Language string        `json:"language"`
		Messages []Translation `json:"messages"`
	}
	if err := json.Unmarshal(jsonFile, &translations); err != nil {
		panic(err)
	}
	for _, t := range translations.Messages { // Replace exact match of the message with the translation
		xmlBytes = bytes.ReplaceAll(xmlBytes, []byte(`"`+t.Message+`"`), []byte(`"`+t.Translation+`"`))
	}

	// Save the modified XML to a file
	ext := path.Ext(os.Args[1])
	err := ioutil.WriteFile(os.Args[1][:len(ext)-1]+"_translated."+ext, xmlBytes, 0644)
	if err != nil {
		fmt.Println("Error writing output XML:", err)
		return
	}

	fmt.Println("Translation applied successfully. Output saved")
}
