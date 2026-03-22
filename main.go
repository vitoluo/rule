package main

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/v2fly/v2ray-core/v5/app/router/routercommon"
	"google.golang.org/protobuf/proto"
)

func main() {
	// 1. Download geosite.dat
	url := "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geosite.dat"
	fmt.Printf("Downloading %s...\n", url)
	resp, err := http.Get(url)
	if err != nil {
		fmt.Printf("Error downloading geosite.dat: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("Error reading download body: %v\n", err)
		os.Exit(1)
	}

	// 2. Parse geosite.dat
	fmt.Println("Parsing geosite.dat...")
	var geoSiteList routercommon.GeoSiteList
	if err := proto.Unmarshal(data, &geoSiteList); err != nil {
		fmt.Printf("Error unmarshaling geosite.dat: %v\n", err)
		os.Exit(1)
	}

	// 3. Read convert-rule.list
	rulesToConvert, err := readRulesToConvert("convert-rule.list")
	if err != nil {
		fmt.Printf("Error reading convert-rule.list: %v\n", err)
		os.Exit(1)
	}

	// 4. Create loon directory if not exists
	os.MkdirAll("loon", 0755)

	// 5. Convert and write rules
	for _, ruleName := range rulesToConvert {
		parts := strings.Split(ruleName, "@")
		siteName := parts[0]
		attrName := ""
		if len(parts) > 1 {
			attrName = parts[1]
		}

		fmt.Printf("Converting %s...\n", ruleName)
		var domains []string
		found := false
		for _, entry := range geoSiteList.Entry {
			if strings.EqualFold(entry.CountryCode, siteName) {
				found = true
				for _, domain := range entry.Domain {
					if attrName != "" {
						matchAttr := false
						for _, attr := range domain.Attribute {
							if strings.EqualFold(attr.Key, attrName) {
								matchAttr = true
								break
							}
						}
						if !matchAttr {
							continue
						}
					}

					var line string
					switch domain.Type {
					case routercommon.Domain_Plain:
						line = "DOMAIN-SUFFIX," + domain.Value
					case routercommon.Domain_Regex:
						// Loon doesn't support regular expressions in standard domain lists, 
						// but it might support URL-REGEX. However, for geosite, regex is often problematic.
						// We'll skip or mark it.
						fmt.Printf("Skipping regex rule in %s: %s\n", ruleName, domain.Value)
						continue
					case routercommon.Domain_RootDomain:
						line = "DOMAIN-SUFFIX," + domain.Value
					case routercommon.Domain_Full:
						line = "DOMAIN," + domain.Value
					default:
						line = "DOMAIN-SUFFIX," + domain.Value
					}
					domains = append(domains, line)
				}
				break
			}
		}

		if !found {
			fmt.Printf("Warning: Rule %s not found in geosite.dat\n", siteName)
			continue
		}

		if len(domains) > 0 {
			outputPath := fmt.Sprintf("loon/%s.list", ruleName)
			err := os.WriteFile(outputPath, []byte(strings.Join(domains, "\n")), 0644)
			if err != nil {
				fmt.Printf("Error writing %s: %v\n", outputPath, err)
			} else {
				fmt.Printf("Saved %s\n", outputPath)
			}
		}
	}
}

func readRulesToConvert(filename string) ([]string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var rules []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			rules = append(rules, line)
		}
	}
	return rules, scanner.Err()
}
