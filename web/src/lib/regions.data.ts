/**
 * 全国省市两级区划数据（34 个省级 / 344 个市）。
 *
 * 本文件由 scripts/gen-regions.mjs 生成，**不要手改** —— 改这里会被下一次
 * 生成覆盖。要改数据改那个脚本，然后 `npm run gen:regions`。
 *
 * 来源：npm 包 province-city-china@8.5.8（国标 GB/T 2260，
 * 民政部 / 国家统计局）。源文件校验和：
 *   province.json 7d7ee447e058bec7
 *   city.json     3356e514ab59493e
 *
 * 两条处理规则（详见生成脚本）：
 *   1. 四个直辖市与港澳台在上游数据里没有地级条目，这里让省自己当自己的市，
 *      所以 310000 既是省码也是市码。
 *   2. 市名去掉了「市」「地区」后缀，保留「盟」「自治州」；吉林市例外
 *      （去掉会与吉林省撞名）。
 */

export interface RegionCity {
  readonly code: number
  readonly name: string
}

export interface RegionProvince {
  readonly code: number
  readonly name: string
  readonly cities: readonly RegionCity[]
}

export const PROVINCES: readonly RegionProvince[] = [
  { code: 110000, name: "北京", cities: [{ code: 110000, name: "北京" }] },
  { code: 120000, name: "天津", cities: [{ code: 120000, name: "天津" }] },
  { code: 130000, name: "河北", cities: [{ code: 130100, name: "石家庄" }, { code: 130200, name: "唐山" }, { code: 130300, name: "秦皇岛" }, { code: 130400, name: "邯郸" }, { code: 130500, name: "邢台" }, { code: 130600, name: "保定" }, { code: 130700, name: "张家口" }, { code: 130800, name: "承德" }, { code: 130900, name: "沧州" }, { code: 131000, name: "廊坊" }, { code: 131100, name: "衡水" }] },
  { code: 140000, name: "山西", cities: [{ code: 140100, name: "太原" }, { code: 140200, name: "大同" }, { code: 140300, name: "阳泉" }, { code: 140400, name: "长治" }, { code: 140500, name: "晋城" }, { code: 140600, name: "朔州" }, { code: 140700, name: "晋中" }, { code: 140800, name: "运城" }, { code: 140900, name: "忻州" }, { code: 141000, name: "临汾" }, { code: 141100, name: "吕梁" }] },
  { code: 150000, name: "内蒙古", cities: [{ code: 150100, name: "呼和浩特" }, { code: 150200, name: "包头" }, { code: 150300, name: "乌海" }, { code: 150400, name: "赤峰" }, { code: 150500, name: "通辽" }, { code: 150600, name: "鄂尔多斯" }, { code: 150700, name: "呼伦贝尔" }, { code: 150800, name: "巴彦淖尔" }, { code: 150900, name: "乌兰察布" }, { code: 152200, name: "兴安盟" }, { code: 152500, name: "锡林郭勒盟" }, { code: 152900, name: "阿拉善盟" }] },
  { code: 210000, name: "辽宁", cities: [{ code: 210100, name: "沈阳" }, { code: 210200, name: "大连" }, { code: 210300, name: "鞍山" }, { code: 210400, name: "抚顺" }, { code: 210500, name: "本溪" }, { code: 210600, name: "丹东" }, { code: 210700, name: "锦州" }, { code: 210800, name: "营口" }, { code: 210900, name: "阜新" }, { code: 211000, name: "辽阳" }, { code: 211100, name: "盘锦" }, { code: 211200, name: "铁岭" }, { code: 211300, name: "朝阳" }, { code: 211400, name: "葫芦岛" }] },
  { code: 220000, name: "吉林", cities: [{ code: 220100, name: "长春" }, { code: 220200, name: "吉林市" }, { code: 220300, name: "四平" }, { code: 220400, name: "辽源" }, { code: 220500, name: "通化" }, { code: 220600, name: "白山" }, { code: 220700, name: "松原" }, { code: 220800, name: "白城" }, { code: 222400, name: "延边朝鲜族自治州" }] },
  { code: 230000, name: "黑龙江", cities: [{ code: 230100, name: "哈尔滨" }, { code: 230200, name: "齐齐哈尔" }, { code: 230300, name: "鸡西" }, { code: 230400, name: "鹤岗" }, { code: 230500, name: "双鸭山" }, { code: 230600, name: "大庆" }, { code: 230700, name: "伊春" }, { code: 230800, name: "佳木斯" }, { code: 230900, name: "七台河" }, { code: 231000, name: "牡丹江" }, { code: 231100, name: "黑河" }, { code: 231200, name: "绥化" }, { code: 232700, name: "大兴安岭" }] },
  { code: 310000, name: "上海", cities: [{ code: 310000, name: "上海" }] },
  { code: 320000, name: "江苏", cities: [{ code: 320100, name: "南京" }, { code: 320200, name: "无锡" }, { code: 320300, name: "徐州" }, { code: 320400, name: "常州" }, { code: 320500, name: "苏州" }, { code: 320600, name: "南通" }, { code: 320700, name: "连云港" }, { code: 320800, name: "淮安" }, { code: 320900, name: "盐城" }, { code: 321000, name: "扬州" }, { code: 321100, name: "镇江" }, { code: 321200, name: "泰州" }, { code: 321300, name: "宿迁" }] },
  { code: 330000, name: "浙江", cities: [{ code: 330100, name: "杭州" }, { code: 330200, name: "宁波" }, { code: 330300, name: "温州" }, { code: 330400, name: "嘉兴" }, { code: 330500, name: "湖州" }, { code: 330600, name: "绍兴" }, { code: 330700, name: "金华" }, { code: 330800, name: "衢州" }, { code: 330900, name: "舟山" }, { code: 331000, name: "台州" }, { code: 331100, name: "丽水" }] },
  { code: 340000, name: "安徽", cities: [{ code: 340100, name: "合肥" }, { code: 340200, name: "芜湖" }, { code: 340300, name: "蚌埠" }, { code: 340400, name: "淮南" }, { code: 340500, name: "马鞍山" }, { code: 340600, name: "淮北" }, { code: 340700, name: "铜陵" }, { code: 340800, name: "安庆" }, { code: 341000, name: "黄山" }, { code: 341100, name: "滁州" }, { code: 341200, name: "阜阳" }, { code: 341300, name: "宿州" }, { code: 341500, name: "六安" }, { code: 341600, name: "亳州" }, { code: 341700, name: "池州" }, { code: 341800, name: "宣城" }] },
  { code: 350000, name: "福建", cities: [{ code: 350100, name: "福州" }, { code: 350200, name: "厦门" }, { code: 350300, name: "莆田" }, { code: 350400, name: "三明" }, { code: 350500, name: "泉州" }, { code: 350600, name: "漳州" }, { code: 350700, name: "南平" }, { code: 350800, name: "龙岩" }, { code: 350900, name: "宁德" }] },
  { code: 360000, name: "江西", cities: [{ code: 360100, name: "南昌" }, { code: 360200, name: "景德镇" }, { code: 360300, name: "萍乡" }, { code: 360400, name: "九江" }, { code: 360500, name: "新余" }, { code: 360600, name: "鹰潭" }, { code: 360700, name: "赣州" }, { code: 360800, name: "吉安" }, { code: 360900, name: "宜春" }, { code: 361000, name: "抚州" }, { code: 361100, name: "上饶" }] },
  { code: 370000, name: "山东", cities: [{ code: 370100, name: "济南" }, { code: 370200, name: "青岛" }, { code: 370300, name: "淄博" }, { code: 370400, name: "枣庄" }, { code: 370500, name: "东营" }, { code: 370600, name: "烟台" }, { code: 370700, name: "潍坊" }, { code: 370800, name: "济宁" }, { code: 370900, name: "泰安" }, { code: 371000, name: "威海" }, { code: 371100, name: "日照" }, { code: 371300, name: "临沂" }, { code: 371400, name: "德州" }, { code: 371500, name: "聊城" }, { code: 371600, name: "滨州" }, { code: 371700, name: "菏泽" }] },
  { code: 410000, name: "河南", cities: [{ code: 410100, name: "郑州" }, { code: 410200, name: "开封" }, { code: 410300, name: "洛阳" }, { code: 410400, name: "平顶山" }, { code: 410500, name: "安阳" }, { code: 410600, name: "鹤壁" }, { code: 410700, name: "新乡" }, { code: 410800, name: "焦作" }, { code: 410900, name: "濮阳" }, { code: 411000, name: "许昌" }, { code: 411100, name: "漯河" }, { code: 411200, name: "三门峡" }, { code: 411300, name: "南阳" }, { code: 411400, name: "商丘" }, { code: 411500, name: "信阳" }, { code: 411600, name: "周口" }, { code: 411700, name: "驻马店" }, { code: 419000, name: "省直辖县级行政区划" }] },
  { code: 420000, name: "湖北", cities: [{ code: 420100, name: "武汉" }, { code: 420200, name: "黄石" }, { code: 420300, name: "十堰" }, { code: 420500, name: "宜昌" }, { code: 420600, name: "襄阳" }, { code: 420700, name: "鄂州" }, { code: 420800, name: "荆门" }, { code: 420900, name: "孝感" }, { code: 421000, name: "荆州" }, { code: 421100, name: "黄冈" }, { code: 421200, name: "咸宁" }, { code: 421300, name: "随州" }, { code: 422800, name: "恩施土家族苗族自治州" }, { code: 429000, name: "省直辖县级行政区划" }] },
  { code: 430000, name: "湖南", cities: [{ code: 430100, name: "长沙" }, { code: 430200, name: "株洲" }, { code: 430300, name: "湘潭" }, { code: 430400, name: "衡阳" }, { code: 430500, name: "邵阳" }, { code: 430600, name: "岳阳" }, { code: 430700, name: "常德" }, { code: 430800, name: "张家界" }, { code: 430900, name: "益阳" }, { code: 431000, name: "郴州" }, { code: 431100, name: "永州" }, { code: 431200, name: "怀化" }, { code: 431300, name: "娄底" }, { code: 433100, name: "湘西土家族苗族自治州" }] },
  { code: 440000, name: "广东", cities: [{ code: 440100, name: "广州" }, { code: 440200, name: "韶关" }, { code: 440300, name: "深圳" }, { code: 440400, name: "珠海" }, { code: 440500, name: "汕头" }, { code: 440600, name: "佛山" }, { code: 440700, name: "江门" }, { code: 440800, name: "湛江" }, { code: 440900, name: "茂名" }, { code: 441200, name: "肇庆" }, { code: 441300, name: "惠州" }, { code: 441400, name: "梅州" }, { code: 441500, name: "汕尾" }, { code: 441600, name: "河源" }, { code: 441700, name: "阳江" }, { code: 441800, name: "清远" }, { code: 441900, name: "东莞" }, { code: 442000, name: "中山" }, { code: 445100, name: "潮州" }, { code: 445200, name: "揭阳" }, { code: 445300, name: "云浮" }] },
  { code: 450000, name: "广西", cities: [{ code: 450100, name: "南宁" }, { code: 450200, name: "柳州" }, { code: 450300, name: "桂林" }, { code: 450400, name: "梧州" }, { code: 450500, name: "北海" }, { code: 450600, name: "防城港" }, { code: 450700, name: "钦州" }, { code: 450800, name: "贵港" }, { code: 450900, name: "玉林" }, { code: 451000, name: "百色" }, { code: 451100, name: "贺州" }, { code: 451200, name: "河池" }, { code: 451300, name: "来宾" }, { code: 451400, name: "崇左" }] },
  { code: 460000, name: "海南", cities: [{ code: 460100, name: "海口" }, { code: 460200, name: "三亚" }, { code: 460300, name: "三沙" }, { code: 460400, name: "儋州" }, { code: 469000, name: "省直辖县级行政区划" }] },
  { code: 500000, name: "重庆", cities: [{ code: 500000, name: "重庆" }] },
  { code: 510000, name: "四川", cities: [{ code: 510100, name: "成都" }, { code: 510300, name: "自贡" }, { code: 510400, name: "攀枝花" }, { code: 510500, name: "泸州" }, { code: 510600, name: "德阳" }, { code: 510700, name: "绵阳" }, { code: 510800, name: "广元" }, { code: 510900, name: "遂宁" }, { code: 511000, name: "内江" }, { code: 511100, name: "乐山" }, { code: 511300, name: "南充" }, { code: 511400, name: "眉山" }, { code: 511500, name: "宜宾" }, { code: 511600, name: "广安" }, { code: 511700, name: "达州" }, { code: 511800, name: "雅安" }, { code: 511900, name: "巴中" }, { code: 512000, name: "资阳" }, { code: 513200, name: "阿坝藏族羌族自治州" }, { code: 513300, name: "甘孜藏族自治州" }, { code: 513400, name: "凉山彝族自治州" }] },
  { code: 520000, name: "贵州", cities: [{ code: 520100, name: "贵阳" }, { code: 520200, name: "六盘水" }, { code: 520300, name: "遵义" }, { code: 520400, name: "安顺" }, { code: 520500, name: "毕节" }, { code: 520600, name: "铜仁" }, { code: 522300, name: "黔西南布依族苗族自治州" }, { code: 522600, name: "黔东南苗族侗族自治州" }, { code: 522700, name: "黔南布依族苗族自治州" }] },
  { code: 530000, name: "云南", cities: [{ code: 530100, name: "昆明" }, { code: 530300, name: "曲靖" }, { code: 530400, name: "玉溪" }, { code: 530500, name: "保山" }, { code: 530600, name: "昭通" }, { code: 530700, name: "丽江" }, { code: 530800, name: "普洱" }, { code: 530900, name: "临沧" }, { code: 532300, name: "楚雄彝族自治州" }, { code: 532500, name: "红河哈尼族彝族自治州" }, { code: 532600, name: "文山壮族苗族自治州" }, { code: 532800, name: "西双版纳傣族自治州" }, { code: 532900, name: "大理白族自治州" }, { code: 533100, name: "德宏傣族景颇族自治州" }, { code: 533300, name: "怒江傈僳族自治州" }, { code: 533400, name: "迪庆藏族自治州" }] },
  { code: 540000, name: "西藏", cities: [{ code: 540100, name: "拉萨" }, { code: 540200, name: "日喀则" }, { code: 540300, name: "昌都" }, { code: 540400, name: "林芝" }, { code: 540500, name: "山南" }, { code: 540600, name: "那曲" }, { code: 542500, name: "阿里" }] },
  { code: 610000, name: "陕西", cities: [{ code: 610100, name: "西安" }, { code: 610200, name: "铜川" }, { code: 610300, name: "宝鸡" }, { code: 610400, name: "咸阳" }, { code: 610500, name: "渭南" }, { code: 610600, name: "延安" }, { code: 610700, name: "汉中" }, { code: 610800, name: "榆林" }, { code: 610900, name: "安康" }, { code: 611000, name: "商洛" }] },
  { code: 620000, name: "甘肃", cities: [{ code: 620100, name: "兰州" }, { code: 620200, name: "嘉峪关" }, { code: 620300, name: "金昌" }, { code: 620400, name: "白银" }, { code: 620500, name: "天水" }, { code: 620600, name: "武威" }, { code: 620700, name: "张掖" }, { code: 620800, name: "平凉" }, { code: 620900, name: "酒泉" }, { code: 621000, name: "庆阳" }, { code: 621100, name: "定西" }, { code: 621200, name: "陇南" }, { code: 622900, name: "临夏回族自治州" }, { code: 623000, name: "甘南藏族自治州" }] },
  { code: 630000, name: "青海", cities: [{ code: 630100, name: "西宁" }, { code: 630200, name: "海东" }, { code: 632200, name: "海北藏族自治州" }, { code: 632300, name: "黄南藏族自治州" }, { code: 632500, name: "海南藏族自治州" }, { code: 632600, name: "果洛藏族自治州" }, { code: 632700, name: "玉树藏族自治州" }, { code: 632800, name: "海西蒙古族藏族自治州" }] },
  { code: 640000, name: "宁夏", cities: [{ code: 640100, name: "银川" }, { code: 640200, name: "石嘴山" }, { code: 640300, name: "吴忠" }, { code: 640400, name: "固原" }, { code: 640500, name: "中卫" }] },
  { code: 650000, name: "新疆", cities: [{ code: 650100, name: "乌鲁木齐" }, { code: 650200, name: "克拉玛依" }, { code: 650400, name: "吐鲁番" }, { code: 650500, name: "哈密" }, { code: 652300, name: "昌吉回族自治州" }, { code: 652700, name: "博尔塔拉蒙古自治州" }, { code: 652800, name: "巴音郭楞蒙古自治州" }, { code: 652900, name: "阿克苏" }, { code: 653000, name: "克孜勒苏柯尔克孜自治州" }, { code: 653100, name: "喀什" }, { code: 653200, name: "和田" }, { code: 654000, name: "伊犁哈萨克自治州" }, { code: 654200, name: "塔城" }, { code: 654300, name: "阿勒泰" }, { code: 659000, name: "自治区直辖县级行政区划" }] },
  { code: 710000, name: "台湾", cities: [{ code: 710000, name: "台湾" }] },
  { code: 810000, name: "香港", cities: [{ code: 810000, name: "香港" }] },
  { code: 820000, name: "澳门", cities: [{ code: 820000, name: "澳门" }] },
]
