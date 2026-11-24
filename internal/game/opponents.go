package game

// Opponent is the shared catalogue entry used by both browser and server.
// The private profile stays in Go; the UI only receives its explanation.
type Opponent struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Group       string        `json:"group"`
	Description string        `json:"description"`
	Search      *SearchConfig `json:"search,omitempty"`
	profile     cpuStyle
}

type cpuStyle struct {
	capture, save, threat              float64
	line, ownDistance, enemyDistance   int
	lineWeight, ownWeight, enemyWeight float64
	cornerWeight, stability            float64
	chase, scatter                     float64
	search                             SearchConfig
}

// Line numbers are zero-based; distances are grid steps. National styles are
// playful presets, and the advanced labels describe algorithms, not real ranks.
var opponentCatalog = []Opponent{
	{
		ID: "beginner-chaser", Name: "追子新手", Group: "入门陪练",
		Description: "总想追着你刚下的子打吃，容易忘记救自己的棋。",
		profile:     cpuStyle{capture: 90, save: 8, threat: 30, line: 2, lineWeight: 1, ownDistance: 3, ownWeight: 0.5, enemyDistance: 1, enemyWeight: 4, cornerWeight: 0.05, chase: 10},
	},
	{
		ID: "beginner-timid", Name: "胆小新手", Group: "入门陪练",
		Description: "爱贴着自己的棋补棋、远离战斗，常把大场让给你。",
		profile:     cpuStyle{capture: 55, save: 100, threat: 1, line: 2, lineWeight: 1, ownDistance: 1, ownWeight: 10, enemyDistance: 5, enemyWeight: 3, cornerWeight: 0.1},
	},
	{
		ID: "beginner-edge", Name: "贴边新手", Group: "入门陪练",
		Description: "觉得边角最安全，偏爱一二线，容易下得低而局促。",
		profile:     cpuStyle{capture: 65, save: 35, threat: 4, line: 0, lineWeight: 10, ownDistance: 2, ownWeight: 1, enemyDistance: 4, enemyWeight: 0.5, cornerWeight: 0.3},
	},
	{
		ID: "beginner-scatter", Name: "随手新手", Group: "入门陪练",
		Description: "东一手西一手，喜欢抢远处空点，却常漏看眼前的攻防。",
		profile:     cpuStyle{capture: 25, save: 12, threat: 2, line: 2, lineWeight: 0.3, ownDistance: 6, ownWeight: 3, enemyDistance: 5, enemyWeight: 0.4, scatter: 55},
	},
	{
		ID: "china", Name: "中国 · 厚势均衡", Group: "经典棋风",
		Description: "偏好四线厚势和友军支援，兼顾提子、救棋与攻守。",
		profile:     cpuStyle{capture: 100, save: 70, threat: 12, line: 3, lineWeight: 2, ownDistance: 2, ownWeight: 2, enemyDistance: 3, enemyWeight: 0.8, cornerWeight: 0.08},
	},
	{
		ID: "japan", Name: "日本 · 布局取地", Group: "经典棋风",
		Description: "偏好三线角地、舒展间距与较少的近身接触。",
		profile:     cpuStyle{capture: 100, save: 70, threat: 8, line: 2, lineWeight: 2, ownDistance: 4, ownWeight: 1.2, enemyDistance: 5, enemyWeight: 0.35, cornerWeight: 0.35},
	},
	{
		ID: "korea", Name: "韩国 · 主动攻杀", Group: "经典棋风",
		Description: "偏好贴身战斗、打吃和主动进攻，积极向你的棋施压。",
		profile:     cpuStyle{capture: 100, save: 70, threat: 20, line: 2, lineWeight: 2, ownDistance: 3, ownWeight: 1.2, enemyDistance: 1, enemyWeight: 2.8, cornerWeight: 0.08},
	},
	{
		ID: "club-balanced", Name: "棋社 · 稳健派", Group: "棋社棋友",
		Description: "更重视救棋、气和稳健连接，避免棋形过紧。",
		profile:     cpuStyle{capture: 105, save: 95, threat: 10, line: 3, lineWeight: 2, ownDistance: 2, ownWeight: 2.5, enemyDistance: 3, enemyWeight: 1, cornerWeight: 0.1, stability: 4},
	},
	{
		ID: "club-fighter", Name: "棋社 · 战斗派", Group: "棋社棋友",
		Description: "重视打吃和逼近弱棋，也会兼顾自己棋块的气。",
		profile:     cpuStyle{capture: 110, save: 85, threat: 28, line: 3, lineWeight: 2, ownDistance: 3, ownWeight: 1.5, enemyDistance: 2, enemyWeight: 2, cornerWeight: 0.05, stability: 2},
	},
	{
		ID: "pro-territory", Name: "研究型 · 实地派", Group: "进阶搜索",
		Description: "以角边实地为先验，有限搜索你的反击，挑选较稳妥的着点。",
		profile:     cpuStyle{capture: 100, save: 90, threat: 14, line: 2, lineWeight: 2.5, ownDistance: 4, ownWeight: 1.5, enemyDistance: 4, enemyWeight: 0.8, cornerWeight: 0.4, stability: 3, search: specialistSearch},
	},
	{
		ID: "pro-influence", Name: "研究型 · 厚势派", Group: "进阶搜索",
		Description: "以四线、支援和宽气为先验，搜索局部攻防中的稳健选择。",
		profile:     cpuStyle{capture: 100, save: 95, threat: 18, line: 3, lineWeight: 2.5, ownDistance: 2, ownWeight: 1.7, enemyDistance: 3, enemyWeight: 1, cornerWeight: 0.02, stability: 4, search: specialistSearch},
	},
	{
		ID: "alphago", Name: "神经棋手", Group: "进阶搜索",
		Description: "从 GNU Go 对局中学习选点，再搜索对手的反击。",
		profile:     cpuStyle{capture: 110, save: 100, threat: 20, line: 2, lineWeight: 2, ownDistance: 3, ownWeight: 1.5, enemyDistance: 3, enemyWeight: 1, cornerWeight: 0.1, stability: 4, search: intensiveSearch},
	},
}

// Opponents returns a copy so callers cannot change the built-in catalogue.
func Opponents() []Opponent {
	list := append([]Opponent(nil), opponentCatalog...)
	for i := range list {
		config := (&Game{Style: list[i].ID}).SearchConfig()
		if config.Roots > 0 {
			list[i].Search = &config
		}
	}
	return list
}

func profileFor(id string) cpuStyle {
	for _, opponent := range opponentCatalog {
		if opponent.ID == id {
			return opponent.profile
		}
	}
	return profileFor("china")
}
