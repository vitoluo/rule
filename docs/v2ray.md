## v2ray规则
> 当规则没有前缀时，默认使用 "domain:" 前缀

### regexp：
正则表达式，当此正则表达式匹配目标域名时，该规则生效。例如 "regexp:\\.goo.*\\.com$" 匹配 "www.google.com"、"fonts.googleapis.com"，但不匹配 "google.com"。大小写敏感。

### domain:
子域名，当此域名是目标域名或其子域名时，该规则生效。例如 "domain:xray.com" 匹配 "www.xray.com" 与 "xray.com"，但不匹配 "wxray.com"。

### keyword:
字符串，当此字符串匹配目标域名中任意部分，该规则生效。例如 "keyword:sina.com" 可以匹配 "sina.com"、"sina.com.cn" 和 "www.sina.com"，但不匹配 "sina.cn"。

### full:
完整匹配，当此域名完整匹配目标域名时，该规则生效。例如 "full:xray.com" 匹配 "xray.com" 但不匹配 "www.xray.com"。