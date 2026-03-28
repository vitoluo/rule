package main

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/v2fly/v2ray-core/v5/app/router/routercommon"
	"google.golang.org/protobuf/proto"
)

func main() {
	// 1. Download geosite.dat
	url := "https://ghproxy.net/https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geosite.dat"
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

	parts := strings.Fields(line)
	if len(parts) == 0 {
		return nil
	}

	rawRule := parts[0]
	var attrs []*routercommon.Domain_Attribute
	for i := 1; i < len(parts); i++ {
		attrStr := parts[i]
		if strings.HasPrefix(attrStr, "@") {
			attrs = append(attrs, &routercommon.Domain_Attribute{
				Key: attrStr[1:],
			})
		}
	}

	domain := &routercommon.Domain{
		Attribute: attrs,
	}

	if strings.HasPrefix(rawRule, "full:") {
		domain.Type = routercommon.Domain_Full
		domain.Value = rawRule[5:]
	} else if strings.HasPrefix(rawRule, "domain:") {
		domain.Type = routercommon.Domain_RootDomain
		domain.Value = rawRule[7:]
	} else if strings.HasPrefix(rawRule, "keyword:") {
		domain.Type = routercommon.Domain_Plain
		domain.Value = rawRule[8:]
	} else if strings.HasPrefix(rawRule, "regexp:") {
		domain.Type = routercommon.Domain_Regex
		domain.Value = rawRule[7:]
	} else {
		// Default to domain:
		domain.Type = routercommon.Domain_RootDomain
		domain.Value = rawRule
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

	geoSiteList := &routercommon.GeoSiteList{}

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

		// Sort domains for consistency
		sort.Slice(domains, func(i, j int) bool {
			if domains[i].Type != domains[j].Type {
				return domains[i].Type < domains[j].Type
			}
			return domains[i].Value < domains[j].Value
		})

		// Write Loon and Clash
		os.WriteFile(filepath.Join("loon", fileName), []byte(strings.Join(loonLines, "\n")), 0644)
		os.WriteFile(filepath.Join("clash", fileName), []byte(strings.Join(clashLines, "\n")), 0644)

		// Add to GeoSiteList
		geoSite := &routercommon.GeoSite{
			CountryCode: siteName,
			Domain:      domains,
		}
		geoSiteList.Entry = append(geoSiteList.Entry, geoSite)

		fmt.Printf("Processed custom rule: %s -> loon, clash, tag: %s\n", fileName, geoSite.CountryCode)
	}

	// Sort GeoSite entries by tag
	sort.Slice(geoSiteList.Entry, func(i, j int) bool {
		return geoSiteList.Entry[i].CountryCode < geoSiteList.Entry[j].CountryCode
	})

	// Write custom.dat
	data, err := proto.Marshal(geoSiteList)
	if err != nil {
		fmt.Printf("Error marshaling custom.dat: %v\n", err)
		return
	}
	err = os.WriteFile("custom.dat", data, 0644)
	if err != nil {
		fmt.Printf("Error writing custom.dat: %v\n", err)
	} else {
		fmt.Println("Successfully generated custom.dat")
	}
}
