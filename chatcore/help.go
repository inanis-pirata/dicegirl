package chatcore

// HelpText is the compact command overview shown by the `help` command. It
// is shared by the CLI and every chat adapter so the two cannot drift; the
// CLI's long usage text stays in the process entry point.
const HelpText = `🎲 dicegirl 指令速查（指令前缀 .。!！ 可省略）

掷骰
  r 3d6+1 [原因]      掷骰；r 单独使用掷默认骰
  rs 2d20kh1          只出总值
  r 3#1d20            连掷 3 轮
  r 2d6>=7            带比较判定
  r &宏名             掷角色卡上保存的宏
  w 10a8 / ww 10a8    骰池（总数 / 每颗点数）

检定与资源
  ra 心理学            技能检定
  ra 困难 心理学        困难/极难/自动成功（由规则书声明）
  ra 心理学+2          奖励骰；-1 惩罚骰
  ra 体质*5            目标值算式
  ra 3#心理学          连扔 3 次
  opp 60 55            对抗检定
  sc 1d10/1d100        理智损失（机器人默认写回）
  rc 幸运 1d10/1d10    资源检定
  en 心理学            成长检定（自动写卡）
  dmg 1d8 [--character 名字]   伤害 + 伤害加值

角色卡
  pc new 张三 --active     建卡并设为当前卡
  pc list / pc show [名字] / pc use 名字 / pc del 名字
  pc nn 新名 / pc copy 旧 新 / pc cpy 目标=来源
  pc redo / pc stat / pc clr / pc val
  st 力量=60 侦查=50      属性/技能赋值
  st 理智-1d6            增减
  st show [键] / st del 键 / st clr
  st &沙漠之鹰=1d10+1d6+3   保存掷骰宏
  dv                      派生数值（HP/MP/理智轨）

人物作成（别名由规则书声明）
  coc / coc6 / coc7 / cocd / coc 10   CoC 建卡
  dnd / dndd                           D&D 5e 建卡

团务与表
  ri +5 张三 / init / init clr   先攻
  deck new 牌堆=卡1|卡2 / draw 牌堆 3
  ak add 选项... / ak get / ak show / ak clr   随机抉择
  ti / li                    临时/总结性疯狂表
  tb 表id / tb list          规则书随机表
  name [cn|jp|en] [数量]      随机姓名
  set [面数]                  设置默认骰面数

规则与系统
  rules 词条 / rules list / rules set 系统
  sys / sys validate
  rh 3d6 / rh .ra 心理学   暗骰：结果只私信给你（首次按提示完成绑定）
  ob join / ob exit / ob list / ob on / ob off   暗骰围观（仅机器人）
  pc tag [名字] / pc untag / pc grp   在本群绑定自己的角色卡（仅机器人）
  log start [标题] / log stop / log status / log show / log list   战报（仅机器人）

表达式：NdM、dM、d%、+ - * / 括号；kh/kl/dh/dl 取高取低；
爆骰 !、重投 r、成功计数 c（如 5d10c>=8）；n# 连掷；>= > <= < 比较。
`
