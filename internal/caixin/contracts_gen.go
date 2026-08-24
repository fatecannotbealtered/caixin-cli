// Package caixin implements the read-only Caixin web API client.
//
// The endpoint tables below are generated from the measured endpoint
// inventory rather than retyped, so the allowlists and display names cannot
// drift.

package caixin

// Upstream endpoints.
const (
	ChannelsUrl           = "https://gateway.caixin.com/api/dataplatform/scroll/category"
	LatestUrl             = "https://gateway.caixin.com/api/dataplatform/scroll/index"
	SearchUrl             = "https://gateway.caixin.com/api/dataplatform/common/search"
	SearchCategoriesUrl   = "https://gateway.caixin.com/api/dataplatform/common/search/category"
	FrontlineListUrl      = "https://k.caixin.com/app/v1/list"
	FrontlineDetailUrl    = "https://k.caixin.com/app/getNewsByCode"
	TopicDirectoryApi     = "https://gateway.caixin.com/api/extapi/homeInterface.jsp"
	BloggersDirectoryUrl  = "https://blog.caixin.com/archive/bloggers"
	CxdataOrigin          = "https://cxdata.caixin.com"
	EntitiesOrigin        = "https://entities.caixin.com"
	NewscrollMenuType     = "WEB_SCROLL_SEARCH"
	NewscrollPageUrl      = "https://www.caixin.com/search/newscroll"
	TopicsCompatUserAgent = "Mozilla/5.0 (compatible; fetch-caixin-news/0.2; user-authorized personal API client)"
	NewscrollPageSize     = 20
)

// BloggersDirectorySorts maps the --sort flag to the upstream path segment.
var BloggersDirectorySorts = map[string]string{
	"latest": "date",
	"pinyin": "pinyin",
}

// CXDataFeed is one of the nine public Caixin Data feeds.
type CXDataFeed struct {
	Name       string
	URL        string
	Referer    string
	Shape      string
	ShowLabels bool
	HasLabels  bool
	PageSize   bool
}

var CXDataFeeds = map[string]CXDataFeed{
	"latest":    {Name: "最新", URL: "https://cxdata.caixin.com/api/dataplus/sjtPc/news", Referer: "https://cxdata.caixin.com/index/newsTab?tab=latest", Shape: "latest", ShowLabels: true, HasLabels: true, PageSize: true},
	"business":  {Name: "商圈", URL: "https://cxdata.caixin.com/api/dataplus/shNews", Referer: "https://cxdata.caixin.com/index/newsTab?tab=business", Shape: "list", ShowLabels: true, HasLabels: true, PageSize: true},
	"hot":       {Name: "热点", URL: "https://cxdata.caixin.com/api/econdata/deepviewTopicHot", Referer: "https://cxdata.caixin.com/index/newsTab?tab=hot", Shape: "hot", ShowLabels: false, HasLabels: false, PageSize: true},
	"depth":     {Name: "深度", URL: "https://cxdata.caixin.com/api/dataplus/sdNews", Referer: "https://cxdata.caixin.com/index/newsTab?tab=depth", Shape: "list", ShowLabels: true, HasLabels: true, PageSize: true},
	"frontline": {Name: "一线", URL: "https://cxdata.caixin.com/api/dataplus/kxNews", Referer: "https://cxdata.caixin.com/index/newsTab?tab=frontline", Shape: "list", ShowLabels: false, HasLabels: true, PageSize: false},
	"graphic":   {Name: "图解", URL: "https://cxdata.caixin.com/api/dataplus/tjNews", Referer: "https://cxdata.caixin.com/index/newsTab?tab=graphic", Shape: "list", ShowLabels: true, HasLabels: true, PageSize: true},
	"research":  {Name: "研究", URL: "https://cxdata.caixin.com/api/dataplus/zkNews", Referer: "https://cxdata.caixin.com/index/newsTab?tab=research", Shape: "list", ShowLabels: true, HasLabels: true, PageSize: true},
	"invest":    {Name: "投融", URL: "https://cxdata.caixin.com/api/dataplus/rzNews", Referer: "https://cxdata.caixin.com/index/newsTab?tab=invest", Shape: "list", ShowLabels: true, HasLabels: true, PageSize: true},
	"selected":  {Name: "数据精选", URL: "https://cxdata.caixin.com/api/dataplus/jxNews", Referer: "https://cxdata.caixin.com/index/jxnews", Shape: "list", ShowLabels: false, HasLabels: true, PageSize: false},
}

// EntityPreview is the single anonymous preview record per entity library.
type EntityPreview struct {
	Name    string
	URL     string
	Referer string
	Params  map[string]string
}

var EntityPreviews = map[string]EntityPreview{
	"companies": {Name: "企业预览", URL: "https://entities.caixin.com/api/companiesNew/app", Referer: "https://entities.caixin.com/companies", Params: map[string]string{"source": "company", "count": "1"}},
	"persons":   {Name: "人物预览", URL: "https://entities.caixin.com/api/personsNew/app", Referer: "https://entities.caixin.com/persons", Params: map[string]string{"count": "1"}},
}

// TopicCategory is one of the six topic-directory entry points. The url is an
// allowlist key: an arbitrary topics url is rejected rather than fetched.
type TopicCategory struct {
	Category string
	Name     string
	Subject  int
}

var TopicCategories = map[string]TopicCategory{
	"https://topics.caixin.com/economy/":       {Category: "economy", Name: "经济专题", Subject: 100300553},
	"https://topics.caixin.com/finance/":       {Category: "finance", Name: "金融专题", Subject: 100300554},
	"https://topics.caixin.com/international/": {Category: "international", Name: "世界专题", Subject: 100300555},
	"https://topics.caixin.com/business/":      {Category: "business", Name: "商业专题", Subject: 100300556},
	"https://topics.caixin.com/opinion/":       {Category: "opinion", Name: "观点专题", Subject: 100300557},
	"https://topics.caixin.com/china_sp/":      {Category: "china", Name: "政经专题", Subject: 100303642},
}

// SearchFilterCodes are the accepted --filter values.
var SearchFilterCodes = map[string]bool{
	"all":    true,
	"author": true,
	"text":   true,
	"title":  true,
}
