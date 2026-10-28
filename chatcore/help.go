package chatcore

// HelpText is the compact command overview shown by the `help` command. It
// is shared by the CLI and every chat adapter so the two cannot drift; the
// CLI's long usage text stays in the process entry point.
const HelpText = `🎲 dicegirl 指令速查（指令前缀 .。!！ 可省略）

掷骰
  r 3d6+1 [原因]      掷骰；r 单独使用掷默认骰
  rd / rd6            默认骰 / 指定面数的默认骰
  rs 2d20kh1          只出总值
  r 3#1d20            连掷 3 轮
  r 2d6>=7            带比较判定
  r 1d3+db            伤害加值自动展开
  r &宏名             掷角色卡上保存的宏
  w 10a8 / ww 10a8    骰池（总数 / 每颗点数）

检定与资源
  ra 心理学            技能检定；rc 是同一检定的 Dice! 别名
  ra 理智              资源轨检定（名字/难度/奖励骰均可用）
  ra 困难 心理学        困难/极难/自动成功（由规则书声明）
  ra 心理学+2          奖励骰；-1 惩罚骰
  rb 心理学 / rp2 60   奖励骰 / 2 个惩罚骰
  ra 体质*5            目标值算式
  ra 3#心理学          连扔 3 次
  rav 侦查 60          对抗检定（名字或数值）
  ds / ds reset        死亡豁免（5e；掷 d20 并记录成功/失败计数）
  sc 1d10/1d100        理智损失（机器人默认写回）
  en 心理学            成长检定（自动写卡）

角色卡
  pc new 张三 STR=50 CON=60  建卡（默认设为当前卡并初始化状态轨）
  pc list / pc show [名字|id] / pc use 名字|id / pc del 名字|id
  pc nn 新名 / pc copy 旧 新 / pc cpy 目标=来源
  pc redo / pc stat / pc clr / pc val
  st 力量60 侦查50        属性/技能赋值（= 可省略）
  st san=40              资源轨赋值（设当前值）
  st 理智-1d6            增减（键可用轨道名：理智/HP/SAN...）
  st show [键] / st del 键 / st clr
  st &沙漠之鹰=1d10+1d6+3   保存掷骰宏
  st derived [--init]     派生数值（HP/MP/理智轨）
  st alloc                技能加点池（作成后分配职业/兴趣点数）
  st alloc 职业 侦查+40    加点（-退点；点数须为整数；clr 重置）

人物作成（别名由规则书声明）
  coc / coc6 / coc7 / cocd / coc 10   CoC 建卡
  dnd / dndd                           D&D 5e 建卡

游戏（团）管理
  game / game list / game show|state [名字|id]  查看当前/列表/详情
  game new [团名] [--system 规则书]     开团（创建者自动成为 GM）
  game join / game exit                玩家上桌 / 退出（GM 也可用 exit）
  game master                          自任 GM；已是 GM 时列出名单
  game set 变量 [值] / game set 变量=值  团变量（GM）
  game call                            点名全部玩家（GM）
  game kick QQ|名字                    把玩家踢下桌（GM）
  game close / game open               暂时关闭 / 重新展开游戏领域（GM）
  game over                            结团：清空名单、变量、轮盘、先攻、牌堆与当前卡指针（GM）
  game rou 面数[*份数]                 启用轮盘骰（GM，面数×份数≤100）
  game rou hist|reset|clr              抽取记录 / 复原袋子 / 清除轮盘
  game use 名字 / game system 规则书 / game del 名字
  说明：每个群一个独立数据根；一个群内可开多个游戏，角色卡共享，
        规则书/先攻/牌堆/绑卡/团务数据按游戏隔离

跑团记录
  log / log new [名称] / log on|off / log end|halt   状态/开始/暂停或继续/结束或中止
  log list [页码] / log stat [名称] / log del 名称
  log get|export [名称]   导出 txt/html（机器人发文件，CLI 显示路径）
  log type txt|html / log timer on|off / log time

先攻/牌堆
  ri +5 张三 / init / init clr   先攻
  deck new 牌堆=卡1|卡2          新建牌堆（::3::卡 表示重复三张）
  deck show [牌堆] / deck reset 牌堆 / deck clr
  deck put 牌堆 卡面...        把卡从弃牌堆放回并重洗
  draw 牌堆 [3] [放回]         抽牌（默认进弃牌堆；放回=抽后放回）
  drawh 牌堆 [3]              暗抽：结果只私信抽牌人（仅机器人）
  ak add 选项... / ak get / ak show / ak clr   随机抉择
  ti / li                    疯狂表（由规则书 command_bindings 绑定）
  tb 表名|表id / tb list     规则书随机表
  name [cn|jp|en] [数量]      随机姓名
  set [面数] / set list / set 规则书   默认骰面数 / 规则书列表与切换

规则与系统
  rule list / rule validate              规则书列表 / 重新校验
  rule ref 词条 / rule ref list / rule ref set 系统   规则速查
  rule show all|sections|show <段> <id> / rule new <新ID> --from <基础规则书>
  rule patch '<JSON>' / rule patch --file <路径>     修改 house rules 并热更新
  rule add --file <规则书.yaml> [--force]   安装自定义规则书（QQ：先发文件再 rule add）
  rule del <id>                     卸载用户规则书（内置或在用规则书拒绝删除）
  h/rh 3d6 / h/rh .ra 心理学   暗骰：结果只私信给你（首次按提示完成绑定）
  rah 心理学 / rch 心理学      暗骰检定：结果只私信给你
  ob join / ob exit / ob list / ob on / ob off   暗骰围观（仅机器人）
  pc tag [名字] / pc untag / pc grp   在本群绑定自己的角色卡（仅机器人）

表达式：NdM、dM、d%、+ - * / 括号；kh/kl/dh/dl 取高取低；
爆骰 !、重投 r、成功计数 c（如 5d10c>=8）；n# 连掷；>= > <= < 比较。
`
