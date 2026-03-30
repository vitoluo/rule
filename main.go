package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/oschwald/maxminddb-golang"
	"github.com/v2fly/v2ray-core/v5/app/router/routercommon"
	"google.golang.org/protobuf/proto"
)

type Config struct {
	GeositeURL  string `json:"geosite_url"`
	AsnMmdbURL  string `json:"asn_mmdb_url"`
}

func main() {
	// 0. Load config
	config, err := loadConfig("config.json")
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}

	// 1. Download geosite.dat
	geositeData, err := downloadFile(config.GeositeURL, "tmp/geosite.dat")
	if err != nil {
		fmt.Printf("Error downloading/reading geosite.dat: %v\n", err)
	} else {
		// 2. Parse geosite.dat
		fmt.Println("Parsing geosite.dat...")
		var geoSiteList routercommon.GeoSiteList
		if err := proto.Unmarshal(geositeData, &geoSiteList); err != nil {
			fmt.Printf("Error unmarshaling geosite.dat: %v\n", err)
		} else {
			// 3. Read convert-rule.list and convert to loon
			rulesToConvert, err := readRulesToConvert("convert-rule.list")
			if err != nil {
				fmt.Printf("Error reading convert-rule.list: %v\n", err)
			} else {
				os.MkdirAll("tmp/loon", 0755)
				for _, ruleName := range rulesToConvert {
					convertGeositeToLoon(ruleName, &geoSiteList)
				}
			}
		}
	}

	// 4. Download and convert GeoLite2-ASN.mmdb
	_, err = downloadFile(config.AsnMmdbURL, "tmp/GeoLite2-ASN.mmdb")
	if err != nil {
		fmt.Printf("Error downloading/reading GeoLite2-ASN.mmdb: %v\n", err)
	} else {
		fmt.Println("Converting GeoLite2-ASN.mmdb to asn.dat...")
		err = convertAsnToDat("tmp/GeoLite2-ASN.mmdb", "tmp/asn.dat")
		if err != nil {
			fmt.Printf("Error converting ASN to dat: %v\n", err)
		} else {
			fmt.Println("Successfully generated tmp/asn.dat")
		}
	}

	// 5. Process custom rules
	fmt.Println("Processing custom rules...")
	processCustomRules()
}

func loadConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var config Config
	err = json.Unmarshal(data, &config)
	return &config, err
}

func downloadFile(url string, cachePath string) ([]byte, error) {
	// Check cache
	if data, err := os.ReadFile(cachePath); err == nil {
		fmt.Printf("Using cached file: %s\n", cachePath)
		return data, nil
	}

	// Download
	fmt.Printf("Downloading %s...\n", url)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad status: %s", resp.Status)
	}

	os.MkdirAll(filepath.Dir(cachePath), 0755)
	
	out, err := os.Create(cachePath)
	if err != nil {
		return nil, err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return nil, err
	}

	return os.ReadFile(cachePath)
}

func convertAsnToDat(mmdbPath, outputPath string) error {
	db, err := maxminddb.Open(mmdbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	type ASNRecord struct {
		ASN uint32 `maxminddb:"autonomous_system_number"`
	}

	networks := db.Networks(maxminddb.SkipAliasedNetworks)
	asnMap := make(map[uint32][]*routercommon.CIDR)

	for networks.Next() {
		var record ASNRecord
		network, err := networks.Network(&record)
		if err != nil {
			return err
		}

		if record.ASN == 0 {
			continue
		}

		ip := network.IP
		if ip4 := ip.To4(); ip4 != nil {
			ip = ip4
		}

		prefix, _ := network.Mask.Size()
		cidr := &routercommon.CIDR{
			Ip:     ip,
			Prefix: uint32(prefix),
		}
		asnMap[record.ASN] = append(asnMap[record.ASN], cidr)
	}

	if networks.Err() != nil {
		return networks.Err()
	}

	geoIPList := &routercommon.GeoIPList{}
	for asn, cidrs := range asnMap {
		geoIPList.Entry = append(geoIPList.Entry, &routercommon.GeoIP{
			CountryCode: fmt.Sprintf("%d", asn),
			Cidr:        cidrs,
		})
	}

	// Sort by tag for consistency
	sort.Slice(geoIPList.Entry, func(i, j int) bool {
		return geoIPList.Entry[i].CountryCode < geoIPList.Entry[j].CountryCode
	})

	data, err := proto.Marshal(geoIPList)
	if err != nil {
		return err
	}

	return os.WriteFile(outputPath, data, 0644)
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
		outputPath := fmt.Sprintf("tmp/loon/%s.list", ruleName)
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

	os.MkdirAll("tmp/loon", 0755)
	os.MkdirAll("tmp/clash", 0755)

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
		os.WriteFile(filepath.Join("tmp/loon", fileName), []byte(strings.Join(loonLines, "\n")), 0644)
		os.WriteFile(filepath.Join("tmp/clash", fileName), []byte(strings.Join(clashLines, "\n")), 0644)

		// Add to GeoSiteList
		geoSite := &routercommon.GeoSite{
			CountryCode: siteName,
			Domain:      domains,
		}
		geoSiteList.Entry = append(geoSiteList.Entry, geoSite)

		fmt.Printf("Processed custom rule: %s -> tmp/loon, tmp/clash, tag: %s\n", fileName, geoSite.CountryCode)
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
	err = os.WriteFile("tmp/custom.dat", data, 0644)
	if err != nil {
		fmt.Printf("Error writing custom.dat: %v\n", err)
	} else {
		fmt.Println("Successfully generated tmp/custom.dat")
	}
}
