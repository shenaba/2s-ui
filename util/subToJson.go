package util

import (
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/shenaba/2s-ui/logger"
	"github.com/shenaba/2s-ui/util/common"
)

// maxExternalSubBytes bounds one external subscription body.
const maxExternalSubBytes = 8 << 20

// externalSubClient verifies the certificate. InsecureSkipVerify was set
// here, and this is the call least able to afford it: the response is turned
// into the outbound configurations this panel then serves to its own
// subscribers, so whoever can intercept the fetch chooses the servers every
// one of those clients connects to -- and the panel would report success the
// whole time.
//
// This drops support for a subscription source with a self-signed
// certificate, or one reached by bare IP. There is no setting to turn it back
// on, deliberately: a source worth trusting with that is worth a real
// certificate, and a toggle would be found by exactly the operators who
// should not use it.
var externalSubClient = &http.Client{
	Timeout: 10 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
	},
}

func GetExternalLink(url string) string {
	response, err := externalSubClient.Get(url)
	if err != nil {
		logger.Warning("sub: Error making HTTP request:", err)
		return ""
	}
	defer response.Body.Close()

	// The body is an operator-configured URL's answer, which is not this
	// panel's to size: an unbounded ReadAll lets a redirect to a large file --
	// or a source that simply never stops -- take the process's memory with it,
	// on a fetch that runs on every subscription request. Eight mebibytes is
	// far past any real subscription; what does not fit is treated as a source
	// that cannot be read, since a truncated one would be served as if it were
	// the whole list.
	body, err := io.ReadAll(io.LimitReader(response.Body, maxExternalSubBytes+1))
	if err != nil {
		logger.Warning("sub: Error reading response body:", err)
		return ""
	}
	if len(body) > maxExternalSubBytes {
		logger.Warning("sub: external subscription is larger than ",
			maxExternalSubBytes, " bytes, ignoring it: ", url)
		return ""
	}

	data := StrOrBase64Encoded(string(body))
	return data
}

func GetExternalSub(url string) ([]map[string]interface{}, error) {
	var err error
	var result []map[string]interface{}

	if len(url) == 0 {
		return nil, common.NewError("no url")
	}

	data := GetExternalLink(url)
	if len(data) == 0 {
		return nil, common.NewError("no result")
	}

	// if the data is a JSON object
	if strings.HasPrefix(data, "{") && strings.HasSuffix(data, "}") {
		var jsonData map[string]interface{}
		err = json.Unmarshal([]byte(data), &jsonData)
		if err != nil {
			logger.Warning("sub: Error unmarshalling JSON:", err)
			return nil, err
		}
		outbounds, ok := jsonData["outbounds"].([]any)
		if !ok {
			return nil, common.NewError("no outbounds in external subscription")
		}
		for _, outbound := range outbounds {
			outboundMap, ok := outbound.(map[string]interface{})
			if ok && len(outboundMap) > 0 {
				oType, _ := outboundMap["type"].(string)
				switch oType {
				case "urltest":
				case "direct":
				case "selector":
				case "block":
					continue
				default:
					result = append(result, outboundMap)
				}
			}
		}
		if len(result) == 0 {
			return nil, common.NewError("no result")
		}
		return result, nil
	} else {
		// if data is a text
		links := strings.Split(data, "\n")
		for _, link := range links {
			linkToJson, _, err := GetOutbound(link, 0)
			if err == nil {
				result = append(result, *linkToJson)
			}
		}
	}
	if len(result) == 0 {
		return nil, common.NewError("no result")
	}
	return result, nil
}
