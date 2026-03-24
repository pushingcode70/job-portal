package main

// Global Greenhouse Seeds (High probability of Indian hiring)
var GlobalGreenhouse = []string{
	"airbnb", "figma", "stripe", "github", "hashicorp", "doordash", "cloudflare", "anduril", "adobe", "agoda", "spacex", "uber", "roblox", "affirm", "airtable", "discord", "canva", "notion", "databricks", "anthropic", "scaleai", "snowflake", "instacart",
	"openai", "okta", "plaid", "brex", "ramp", "gusto", "coinbase", "kraken", "gemini", "opensea", "binance", "chainlink", "alchemy", "consensys", "circle", "ripple", "fireblocks", "paxos", "blockfi", "bitgo", "datadog", "confluent", "mongodb", "elastic", "splunk", "newrelic", "pagerduty", "sentry", "grafana", "honeycomb", "twilio", "sendgrid", "mailchimp", "klaviyo", "onesignal", "amazon", "google", "meta", "microsoft", "apple", "netflix", "spotify", "tesla", "salesforce", "oracle", "sap", "servicenow", "workday", "zendesk", "atlassian", "hubspot", "intercom", "zoom", "slack", "cisco", "skype", "poly", "unity", "riotgames", "epicgames", "snapchat", "pinterest", "instacart", "wealthfront", "betterment", "clari", "gonivo",

	// JavaScript / React / Node.js heavy companies
	"contentful", "sanity", "storyblok", "prismic", "gatsby", "nextjs-shop", "vercel", "netlify", "hasura", "appwrite", "nhost", "railway",
	"retool", "airplane", "tooljet", "appsmith", "budibase", "forestadmin",
	"shopify", "bigcommerce", "woocommerce", "squarespace", "wix",

	// Java / Spring heavy companies
	"atlassian", "jetbrains", "gradle", "sonatype", "jfrog", "checkmarx",
	"microfocus", "broadcom-ca", "ibm", "redhat", "vmware", "pivotal",
	"thoughtworks", "capgemini", "infosys", "wipro", "tcs", "hcl", "ltimindtree",
	"mphasis", "hexaware", "persistent", "cyient", "sonata-software",

	// C++ / Systems / Game Engine companies
	"unrealengine", "crytek", "activision", "ea-games", "2k", "ubisoft",
	"amd", "nvidia", "qualcomm", "arm", "intel", "marvell",
	"bloomberg", "citadel", "drw", "jane-street", "two-sigma", "optiver",

	// Backend / API heavy companies
	"twilio", "messageBird", "vonage", "agora", "livekit", "daily",
	"redis", "cockroachlabs", "yugabyte", "tidb", "planetscale", "neon",
	"temporal", "conductor", "celonis", "camunda",
	"apigee", "kong", "tyk", "mulesoft", "boomi",

	// Frontend / Design System companies
	"figma", "sketch", "invision", "zeplin", "framer",
	"storybook", "chromatic", "percy", "browserstack", "lambdatest",
}

// Global Lever Seeds
var GlobalLever = []string{
	"lever", "palantir", "twitch", "lyft", "robinhood", "shopify", "framer", "coursera", "duolingo", "yelp", "reddit", "asana", "box", "dropbox", "eventbrite", "quora", "glassdoor", "zapier", "udemy", "edx", "pluralsight", "masterclass", "udacity", "codecademy", "ironhack", "mural", "miro", "loom", "webflow", "cockroachlabs", "fivetran", "dbtlabs", "starburst", "clickhouse", "posthog", "vercel", "netlify", "grafbase", "supabase", "convex", "planetscale",

	// JavaScript / React ecosystem
	"reacttraining", "remix", "tanstack", "cypress", "playwright-dev", "testing-library",
	"storybook", "chromatic", "glitch", "codesandbox", "stackblitz", "replit",
	"gitpod", "coder", "github1s", "devpod",

	// MongoDB / NoSQL companies
	"mongodb-inc", "redis-labs", "couchbase", "datastax", "elastic-co",
	"fauna", "upstash", "momento", "macrometa", "astradb",

	// Java-heavy Indian MNCs on Lever
	"thoughtworks", "virtusa", "zensar", "nagarro", "mastech",
	"niit-technologies", "birlasoft", "tata-elxsi", "kpit",

	// C++/Embedded companies
	"siemens", "bosch", "continental", "denso", "harman",
	"l-and-t-technology", "tata-technologies", "altair", "ansys",

	// Frontend-focused product companies
	"gumroad", "memberstack", "outseta", "senja", "testimonial",
	"headlessui", "radix-ui", "shadcn", "daisyui", "mantine",
	"tremor", "chakra-ui", "primer", "polaris-shopify",

	// Backend/API companies
	"resend", "loops", "useplunk", "listmonk", "emailoctopus",
	"novu", "courier", "knock", "notificationapi", "engagespot",
	"inngest", "trigger-dev", "quirrel", "zeplo",
	"brainboard", "terrateam", "atlantis", "env0", "spacelift",
}

// Comprehensive Seed list for Indian Startups & MNC Hubs
var IndianStartups = []string{
	// High-Volume Fintech
	"zomato", "swiggy", "flipkart", "paytm", "razorpay", "cred", "meesho", "phonepe", "ola", "oyo", "groww", "zerodha", "bharatpe", "slice", "jupiter", "fi-money", "khatabook", "okcredit", "razorpayx", "cashfree", "juspay", "pinelabs", "innoviti", "ezetap", "moneytap", "rupeek", "ring", "moneyview", "kreditbee", "stashfin", "m-pocket", "earlysalary", "cashe", "niyo", "onecard", "fampay", "jar", "smallcase", "indmoney", "cleartax", "paisa-bazaar", "policy-bazaar", "acko", "digit", "turtlemint", "renewbuy",

	// E-commerce & Logistics
	"zepto", "blinkit", "bigbasket", "dunzo", "licious", "boat", "noise", "mamaearth", "purplle", "sugar-cosmetics", "bewakoof", "souled-store", "snitch", "mokobara", "pepperfry", "livspace", "homelane", "wakefit", "sleepycat", "delhivery", "ecom-express", "xpressbees", "shadowfax", "porter", "blackbuck", "rivigo", "elasticrun", "udaan", "moglix", "ofbusiness", "zetwerk", "infra-market",

	// SaaS & DevTools (The "Ashby/Greenhouse" sweet spot)
	"freshworks", "zoho", "darwinbox", "chargebee", "postman", "browserstack", "hasura", "appsmith", "tooljet", "atlan", "hevodata", "signoz", "devrev", "lambdatest", "haptik", "yellow-ai", "gupshup", "webengage", "clevertap", "moengage", "netcore", "wingify", "vwo", "druva", "icertis", "highradius", "mindtickle", "whatfix", "leadsquared", "leena-ai", "facilio", "pando", "locus", "fareye", "shipsy", "shiprocket",

	// EdTech & HealthTech
	"unacademy", "upgrad", "scaler", "eruditus", "physicswallah", "cuemath", "classplus", "testbook", "adda247", "vedantu", "doubtnut", "practo", "pristyncare", "innovaccer", "healthplix", "tata-1mg", "pharmeasy", "netmeds", "medibuddy", "healthify-me", "cult-fit", "cure-fit", "dozee", "ultrahuman", "niramai",

	// Major Global MNC Hubs in India (Often Workday/Greenhouse)
	"amazon-india", "google-india", "walmart-global-tech", "target-india", "tesco-india", "lowes-india", "nike-india", "adidas-india", "visa-india", "mastercard-india", "american-express", "capital-one", "goldman-sachs", "jpmorgan-chase", "morgan-stanley", "wells-fargo", "standard-chartered", "hsbc", "barclays", "db", "societe-generale", "bnp-paribas", "ubs", "citadel", "two-sigma", "jane-street", "hudson-river-trading", "tower-research", "jump-trading", "optiver", "drw", "five-rings", "flow-traders", "citigroup", "bofa", "natwest", "fidelity", "blackrock", "vanguard", "state-street", "schwab", "intuit", "paypal", "ebay", "expedia", "booking", "tripadvisor", "airbnb", "uber", "lyft", "grab", "gojek", "tokopedia", "shopee", "mercari",

	// Semiconductor & Hardware Hubs
	"intel", "nvidia", "qualcomm", "amd", "arm", "broadcom", "marvell", "texas-instruments", "analog-devices", "micron", "western-digital", "seagate", "samsung", "apple", "hp", "dell", "lenovo", "cisco", "juniper", "arista", "f5", "palo-alto-networks", "fortinet", "crowdstrike", "okta", "zscaler",

	// NEW: Indian-based JavaScript / React / MongoDB companies
	"geekyants", "codemonk", "springworks", "skcript", "draftbit", "saiva", "obvious", "hashedin", "sahaj",
	"qubole", "incedo", "kellton", "happiestminds", "mphasis", "zensar", "hexaware", "birlasoft",
	"nagarro", "mastech", "niit-technologies",

	// NEW: Indian Game/C++ hubs
	"nazara", "gameberry", "winzo", "mpl", "dream11", "games24x7",

	// NEW: Indian Frontend-focused product companies
	"razorpay-design", "browserstack", "lambdatest", "appknox", "testfairy",
	"sitecore-india", "drupal-india", "wordpress-india",

	// NEW: Indian Backend / API / Infra companies
	"supertokens", "ory", "permit-io", "cerbos", "casbin",
	"avesha", "platformatory", "infracloud", "civo", "okteto",
	"porter-io", "railway-india", "coolify",
}

// Location tags and keywords for the detection engine
var IndiaCityKeywords = []string{
	"bangalore", "bengaluru", "pune", "hyderabad", "gurgaon", "gurugram", "noida",
	"mumbai", "chennai", "delhi", "ahmedabad", "kolkata", "jaipur", "kochi",
	"thiruvananthapuram", "coimbatore", "indore", "chandigarh", "bhubaneswar",
	"nagpur", "lucknow", "vadodara", "surat", "kanpur", "patna", "visakhapatnam",
	"bhopal", "ludhiana", "agra", "nashik", "faridabad", "meerut", "rajkot",
	"kalyan", "vasai", "virar", "varanasi", "srinagar", "aurangabad", "dhanbad",
	"amritsar", "navi mumbai", "allahabad", "ranchi", "howrah", "jabalpur",
	"gwalior", "vijayawada", "jodhpur", "madurai", "raipur", "kota", "guwahati",
	"solapur", "hubli", "dharwad", "bareilly", "moradabad", "mysore", "gurugram", "ncr",
	"india", "karnataka", "maharashtra", "telangana", "haryana", "tamil nadu", "tamilnadu",
	"west bengal", "rajasthan", "kerala", "punjab", "gujarat", "bihar", "madhya pradesh",
	"uttar pradesh", "andhra pradesh", "odisha", "assam", "kashmir", "goa", "gurgaon", "ncr",
	"mangalore", "belgaum", "gulbarga", "davangere", "bellary", "bijapur", "shimoga", "tumkur",
	"raichur", "bidar", "hospet", "gadag", "hassan", "coorg", "udupi", "panjim", "vasco",
	"pimpri", "chinchwad", "nagpur", "thane", "nashik", "solapur", "aurangabad", "amravati",
	"kolhapur", "sangli", "akola", "nanded", "dhule", "jalgaon", "latur", "secunderabad",
	"warangal", "nizamabad", "karimnagar", "ramagundam", "khammam", "tirupati", "anantapur",
	"kakinada", "guntur", "nellore", "kurnool", "kadapa", "vizianagaram", "eluru",
}

var InternationalHubKeywords = []string{
	"usa", "united states", "uk", "london", "germany", "berlin",
	"poland", "warsaw", "romania", "singapore", "dubai", "uae",
	"australia", "sydney", "canada", "toronto", "vancouver",
	"netherlands", "amsterdam", "brazil", "sao paulo",
	"paris", "france", "tokyo", "japan", "seoul", "korea",
}
