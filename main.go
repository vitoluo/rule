package main

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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
	} else {
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			fmt.Printf("Error reading download body: %v\n", err)
		} else {
			// 2. Parse geosite.dat
			fmt.Println("Parsing geosite.dat...")
			var geoSiteList routercommon.GeoSiteList
			if err := proto.Unmarshal(data, &geoSiteList); err != nil {
				fmt.Printf("Error unmarshaling geosite.dat: %v\n", err)
			} else {
				// 3. Read convert-rule.list and convert to loon
				rulesToConvert, err := readRulesToConvert("convert-rule.list")
				if err != nil {
					fmt.Printf("Error reading convert-rule.list: %v\n", err)
				} else {
					os.MkdirAll("loon", 0755)
					for _, ruleName := range rulesToConvert {
						convertGeositeToLoon(ruleName, &geoSiteList)
					}
				}
			}
		}
	}

	// 4. Process custom rules
	fmt.Println("Processing custom rules...")
	processCustomRules()
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

func convertGeositeToLoon(ruleName string, geoSiteList *routercommon.GeoSiteList) {
	parts := strings.Split(ruleName, "@")
	siteName := parts[0]
	attrName := ""
	if len(parts) > 1 {
		attrName = parts[1]
	}

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

				line := domainToLoon(domain)
				if line != "" {
					domains = append(domains, line)
				}
			}
			break
		}
	}

	if found && len(domains) > 0 {
		outputPath := fmt.Sprintf("loon/%s.list", ruleName)
		os.WriteFile(outputPath, []byte(strings.Join(domains, "\n")), 0644)
		fmt.Printf("Saved %s from geosite.dat\n", outputPath)
	}
}

func domainToLoon(domain *routercommon.Domain) string {
	switch domain.Type {
	case routercommon.Domain_Plain:
		return "DOMAIN-KEYWORD," + domain.Value
	case routercommon.Domain_RootDomain:
		return "DOMAIN-SUFFIX," + domain.Value
	case routercommon.Domain_Full:
		return "DOMAIN," + domain.Value
	default:
		return ""
	}
}

func domainToClash(domain *routercommon.Domain) string {
	switch domain.Type {
	case routercommon.Domain_Plain:
		return "DOMAIN-KEYWORD," + domain.Value
	case routercommon.Domain_RootDomain:
		return "DOMAIN-SUFFIX," + domain.Value
	case routercommon.Domain_Full:
		return "DOMAIN," + domain.Value
	case routercommon.Domain_Regex:
		return "DOMAIN-REGEX," + domain.Value
	default:
		return ""
	}
}

func parseV2RayRule(line string) *routercommon.Domain {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return nil
	}

	domain := &routercommon.Domain{}
	if strings.HasPrefix(line, "full:") {
		domain.Type = routercommon.Domain_Full
		domain.Value = line[5:]
	} else if strings.HasPrefix(line, "domain:") {
		domain.Type = routercommon.Domain_RootDomain
		domain.Value = line[7:]
	} else if strings.HasPrefix(line, "keyword:") {
		domain.Type = routercommon.Domain_Plain
		domain.Value = line[8:]
	} else if strings.HasPrefix(line, "regexp:") {
		domain.Type = routercommon.Domain_Regex
		domain.Value = line[7:]
	} else {
		// Default to domain:
		domain.Type = routercommon.Domain_RootDomain
		domain.Value = line
	}
	return domain
}

func processCustomRules() {
	files, err := os.ReadDir("custom")
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		fmt.Printf("Error reading custom directory: %v\n", err)
		return
	}

	os.MkdirAll("loon", 0755)
	os.MkdirAll("clash", 0755)
	os.MkdirAll("dat", 0755)

	for _, file := range files {
		if file.IsDir() {
			continue
		}
		fileName := file.Name()
		siteName := strings.TrimSuffix(fileName, filepath.Ext(fileName))

		filePath := filepath.Join("custom", fileName)
		content, err := os.ReadFile(filePath)
		if err != nil {
			fmt.Printf("Error reading %s: %v\n", filePath, err)
			continue
		}

		lines := strings.Split(string(content), "\n")
		var domains []*routercommon.Domain
		var loonLines []string
		var clashLines []string

		for _, line := range lines {
			domain := parseV2RayRule(line)
			if domain == nil {
				continue
			}
			domains = append(domains, domain)

			loonRule := domainToLoon(domain)
			if loonRule != "" {
				loonLines = append(loonLines, loonRule)
			}

			clashRule := domainToClash(domain)
			if clashRule != "" {
				clashLines = append(clashLines, clashRule)
			}
		}

		// Write Loon and Clash
		os.WriteFile(filepath.Join("loon", fileName), []byte(strings.Join(loonLines, "\n")), 0644)
		os.WriteFile(filepath.Join("clash", fileName), []byte(strings.Join(clashLines, "\n")), 0644)

		// Write Dat
		geoSite := &routercommon.GeoSite{
			CountryCode: strings.ToUpper(siteName),
			Domain:      domains,
		}
		geoSiteList := &routercommon.GeoSiteList{
			Entry: []*routercommon.GeoSite{geoSite},
		}
		data, err := proto.Marshal(geoSiteList)
		if err != nil {
			fmt.Printf("Error marshaling %s: %v\n", siteName, err)
			continue
		}
		datPath := filepath.Join("dat", siteName+".dat")
		os.WriteFile(datPath, data, 0644)

		fmt.Printf("Processed custom rule: %s -> loon, clash, %s\n", fileName, datPath)
	}
}
