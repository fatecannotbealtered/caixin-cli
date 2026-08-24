package caixin

// Snapshot entry points, generated from the measured endpoint inventory.
//
// This is an allowlist, not a pattern: `snapshot` accepts only the exact page
// entries whose markup has actually been measured. A page shaped differently
// would parse into a confidently wrong listing, so an unmeasured url is
// refused rather than guessed at.
var SnapshotEntrypoints = map[string]string{
	"https://bijiao.caixin.com/":              "bijiao-index",
	"https://blog.caixin.com/":                "blog-index",
	"https://china.caixin.com/":               "china",
	"https://cnreform.caixin.com/":            "cnreform-index",
	"https://companies.caixin.com/":           "companies",
	"https://culture.caixin.com/":             "culture",
	"https://datanews.caixin.com/":            "datanews",
	"https://economy.caixin.com/":             "economy",
	"https://en.caixin.com/":                  "en-index",
	"https://finance.caixin.com/":             "finance",
	"https://international.caixin.com/":       "international",
	"https://mini.caixin.com/":                "mini",
	"https://mini.caixin.com/art/":            "mini-art",
	"https://mini.caixin.com/briefing/":       "mini-briefing",
	"https://mini.caixin.com/diet/":           "mini-diet",
	"https://mini.caixin.com/discussion/":     "mini-discussion",
	"https://mini.caixin.com/film/":           "mini-film",
	"https://mini.caixin.com/health/":         "mini-health",
	"https://mini.caixin.com/reading/":        "mini-reading",
	"https://mini.caixin.com/science/":        "mini-science",
	"https://mini.caixin.com/serial/":         "mini-serial",
	"https://mini.caixin.com/sports/":         "mini-sports",
	"https://mini.caixin.com/story/":          "mini-story",
	"https://mini.caixin.com/travel/":         "mini-travel",
	"https://opinion.caixin.com/":             "opinion",
	"https://other.caixin.com/jx_newsletter/": "newsletter",
	"https://photos.caixin.com/":              "photos-index",
	"https://science.caixin.com/":             "science",
	"https://topics.caixin.com/":              "topics",
	"https://video.caixin.com/":               "video-index",
	"https://weekly.caixin.com/":              "weekly-index",
	"https://wenews.caixin.com/":              "wenews-index",
	"https://www.caixin.com/":                 "home",
	"https://www.caixin.com/auto/":            "auto",
	"https://www.caixin.com/consumer/":        "consumer",
	"https://www.caixin.com/energy/":          "energy",
	"https://www.caixin.com/esg/":             "esg",
	"https://www.caixin.com/health/":          "health",
	"https://www.caixin.com/livelihood/":      "livelihood",
	"https://www.caixin.com/obituary/":        "obituary",
	"https://www.caixin.com/property/":        "property",
	"https://www.caixin.com/tech/":            "tech",
}

// publicationRoots are the magazine-style fronts, which share a card layout.
var publicationRoots = map[string]bool{
	"bijiao-index":   true,
	"cnreform-index": true,
	"weekly-index":   true,
}

// categorySnapshotKeys are the channel fronts that share the comMain layout.
var categorySnapshotKeys = map[string]bool{
	"auto":            true,
	"consumer":        true,
	"energy":          true,
	"livelihood":      true,
	"mini-art":        true,
	"mini-briefing":   true,
	"mini-diet":       true,
	"mini-discussion": true,
	"mini-film":       true,
	"mini-health":     true,
	"mini-reading":    true,
	"mini-science":    true,
	"mini-serial":     true,
	"mini-sports":     true,
	"mini-story":      true,
	"mini-travel":     true,
	"obituary":        true,
	"property":        true,
	"tech":            true,
}

// PublicDirectoryEntrypoints is the exact allowlist of public directory pages.
var PublicDirectoryEntrypoints = map[string]string{
	"https://china.caixin.com/anticorruption-list/": "anticorruption_list",
	"https://china.caixin.com/anticorruption/":      "anticorruption",
	"https://datanews.caixin.com/datatopic/":        "datanews_topics",
	"https://index.caixin.com/esg30/":               "esg30",
	"https://photos.caixin.com/photoreport/":        "photo_week",
	"https://photos.caixin.com/sx/":                 "photo_sight",
	"https://promote.caixin.com/":                   "promote_home",
	"https://promote.caixin.com/topic/":             "promote_topics",
	"https://www.caixin.com/anti_infringement/":     "anti_infringement",
}

// PublicDirectoryAliases folds the spellings the site itself links to.
var PublicDirectoryAliases = map[string]string{
	"http://china.caixin.com/anticorruption-list/": "https://china.caixin.com/anticorruption-list/",
	"http://china.caixin.com/anticorruption/":      "https://china.caixin.com/anticorruption/",
	"http://promote.caixin.com":                    "https://promote.caixin.com/",
	"http://promote.caixin.com/":                   "https://promote.caixin.com/",
	"http://www.caixin.com/anti_infringement/":     "https://www.caixin.com/anti_infringement/",
	"https://promote.caixin.com":                   "https://promote.caixin.com/",
	"https://promote.caixin.com/index.html":        "https://promote.caixin.com/",
	"https://promote.caixin.com/topic":             "https://promote.caixin.com/topic/",
	"https://promote.caixin.com/topic#list_more":   "https://promote.caixin.com/topic/",
}

// Login endpoints, generated from the measured endpoint inventory.
const (
	QRStartURL  = "https://gateway.caixin.com/api/ucenter/scan/v1/genQRCode"
	QRStatusURL = "https://gateway.caixin.com/api/ucenter/scan/v1/checkQRCodeStatus"
	UserInfoURL = "https://gateway.caixin.com/api/ucenter/userinfo/get"
)

// loginCookieFields maps a session cookie to the login response field that
// fills it. Generated, not hand-maintained, so the set cannot drift.
var loginCookieFields = map[string]string{
	"SA_USER_auth":        "userAuth",
	"UID":                 "uid",
	"SA_USER_UID":         "uid",
	"SA_USER_NICK_NAME":   "nickname",
	"SA_USER_UNIT":        "unit",
	"SA_USER_DEVICE_TYPE": "deviceType",
	"USER_LOGIN_CODE":     "code",
	"SA_AUTH_TYPE":        "authType",
}

// Entitlement endpoints, generated from the measured endpoint inventory.
const (
	EntitlementsURL    = "https://gateway.caixin.com/api/app-api/userAuth/getUserUseGoodsTypeV2"
	PowerCatalogURL    = "https://gateway.caixin.com/api/app-api/auth/findPowerByReq"
	SinglePurchasesURL = "https://gateway.caixin.com/api/app-api/auth/getBuyArticleAuthLog"
)

// MicrositePaths is the exact set of standalone microsites, ported
// verbatim. It is a list, not a pattern: the paths share no shape.
var MicrositePaths = map[string]bool{
	"https://economy.caixin.com/2024/boao2024/":                  true,
	"https://economy.caixin.com/2024/developmentforum2024/":      true,
	"https://economy.caixin.com/2025/boao2025/":                  true,
	"https://economy.caixin.com/2025/developmentforum2025/":      true,
	"https://economy.caixin.com/2026/boao2026/":                  true,
	"https://economy.caixin.com/2026/developmentforum2026/":      true,
	"https://international.caixin.com/2025/2025xjdws/":           true,
	"https://international.caixin.com/2026/2026djdws/":           true,
	"https://international.caixin.com/2026/2026xjdws/":           true,
	"https://opinion.caixin.com/2014/caichan/index.html":         true,
	"https://opinion.caixin.com/2014/dengxiaoping/index.html":    true,
	"https://opinion.caixin.com/2014/fanlongduan/index.html":     true,
	"https://opinion.caixin.com/2014/tengxunqihu/index.html":     true,
	"https://opinion.caixin.com/2014/tiruoer/index.html":         true,
	"https://opinion.caixin.com/2014/xianfa/index.html":          true,
	"https://opinion.caixin.com/2014/yuwaifanfu/index.html":      true,
	"https://opinion.caixin.com/2014/zibenlun/index.html":        true,
	"https://opinion.caixin.com/2015/guoqi/index.html":           true,
	"https://opinion.caixin.com/2015/huyaobang/index.html":       true,
	"https://opinion.caixin.com/2015/jihuashengyu/":              true,
	"https://opinion.caixin.com/2018/gaigekaifang/":              true,
	"https://promote.caixin.com/esg2024-march/":                  true,
	"https://promote.caixin.com/esg30-young-scholars-2025/":      true,
	"https://promote.caixin.com/renbenzhineng/":                  true,
	"https://promote.caixin.com/szsd/":                           true,
	"https://topics.caixin.com/2021/2021cxss/":                   true,
	"https://topics.caixin.com/2021/caixin_summit2021/":          true,
	"https://topics.caixin.com/2021/caixin_summit2021sz/":        true,
	"https://topics.caixin.com/2022/2022cxss/":                   true,
	"https://topics.caixin.com/2022/caixin_summit2022/":          true,
	"https://topics.caixin.com/2023/2023cxss/":                   true,
	"https://topics.caixin.com/2023/asia-new-vision-forum/":      true,
	"https://topics.caixin.com/2023/caixin_summit2023/":          true,
	"https://topics.caixin.com/2024/2024cxss/":                   true,
	"https://topics.caixin.com/2024/asia-new-vision-forum_2024/": true,
	"https://topics.caixin.com/2024/caixin_summit_2024/":         true,
	"https://topics.caixin.com/2025/2025cxss/":                   true,
	"https://topics.caixin.com/2025/asia-new-vision-forum_2025/": true,
	"https://topics.caixin.com/2025/caixin_summit2025/":          true,
	"https://topics.caixin.com/2026/2026cxss/":                   true,
}
