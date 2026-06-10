package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/v2fly/v2ray-core/v5/app/router/routercommon"
	"google.golang.org/protobuf/proto"
)

// 查看指定 geosite.dat 文件中包含的所有 site 和 attr
// 用法: go test -run TestListGeoSite -v
func TestListGeoSite(t *testing.T) {
	datPath := "tmp/geosite.dat"

	data, err := os.ReadFile(datPath)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", datPath, err)
	}

	var geoSiteList routercommon.GeoSiteList
	if err := proto.Unmarshal(data, &geoSiteList); err != nil {
		t.Fatalf("解析 geosite.dat 失败: %v", err)
	}

	fmt.Printf("共 %d 个 site\n\n", len(geoSiteList.Entry))

	// 收集所有 site 名称
	siteNames := make([]string, 0, len(geoSiteList.Entry))
	// 收集每个 site 的 attr 集合
	siteAttrs := make(map[string]map[string]int) // site -> attr -> 出现次数

	for _, entry := range geoSiteList.Entry {
		name := entry.CountryCode
		siteNames = append(siteNames, name)

		attrCount := make(map[string]int)
		for _, domain := range entry.Domain {
			for _, attr := range domain.Attribute {
				attrCount[attr.Key]++
			}
		}
		siteAttrs[name] = attrCount
	}

	sort.Strings(siteNames)

	for _, name := range siteNames {
		attrs := siteAttrs[name]
		if len(attrs) == 0 {
			fmt.Printf("  %s (无 attr)\n", name)
		} else {
			// 按出现次数降序排列 attr
			attrKeys := make([]string, 0, len(attrs))
			for k := range attrs {
				attrKeys = append(attrKeys, k)
			}
			sort.Slice(attrKeys, func(i, j int) bool {
				return attrs[attrKeys[i]] > attrs[attrKeys[j]]
			})

			var attrParts []string
			for _, k := range attrKeys {
				attrParts = append(attrParts, fmt.Sprintf("%s(%d)", k, attrs[k]))
			}
			fmt.Printf("  %s @%s\n", name, strings.Join(attrParts, ", "))
		}
	}
}
