package onvif

import (
	"bytes"
	"encoding/xml"
	"io"
)

// xmlWellFormed decodes body fully, returning an error if it is not
// well-formed XML.
func xmlWellFormed(body []byte) error {
	dec := xml.NewDecoder(bytes.NewReader(body))
	for {
		_, err := dec.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
