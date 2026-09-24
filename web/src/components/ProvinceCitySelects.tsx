import { Select } from '@/components/ui/select'
import { citiesOf, cityOptions, PROVINCE_OPTIONS } from '@/lib/regions'

interface ProvinceCitySelectsProps {
  /** 省码。null = 还没选省 */
  province: number | null
  /** 市码。null = 省选了但市还没选 */
  city: number | null
  onProvince: (code: number | null) => void
  /**
   * 市码。会给 null —— 换省的时候上一个省的市必须被清掉，
   * 否则界面上会出现「浙江 · 上海」。
   */
  onCity: (code: number | null) => void
  /** id 前缀，生成 `${idBase}_province` / `${idBase}_city` */
  idBase: string
  disabled?: boolean
  'aria-labelledby'?: string
  'aria-describedby'?: string
}

/**
 * 省 → 市两级联动，两个原生 select。受控、无内部状态。
 *
 * **只剩偏好页在用**（「希望对方在哪些城市」最多 5 个、带「添加」按钮）。
 * 档案里的所在城市与家乡已经换成 RegionPicker —— 一个字段 + 底部弹层的
 * 省市两级滚轮（PRD 5.3「省市两级弹层」）。两者没有合并，是因为交互本来
 * 就不同：那边是「选一个地方存下来」，这边是「挑几个地方加进清单」，
 * 后者的草稿值和 chips 由 preferences.tsx 自己管（见那边 :199-206 的说明）。
 *
 * 换成两级而不是一条「浙江省杭州市」的平铺列表：后者是 344 条，
 * 而两级之后每级只有三十来条。名字在生成脚本里去过后缀（`石家庄市` →
 * `石家庄`），与界面上「上海」「广州」的写法一致。
 */
export function ProvinceCitySelects({
  province,
  city,
  onProvince,
  onCity,
  idBase,
  disabled,
  'aria-labelledby': ariaLabelledby,
  'aria-describedby': ariaDescribedby,
}: ProvinceCitySelectsProps) {
  const cities = citiesOf(province)
  const options = cityOptions(province)

  // 库里存着表里没有的码（老数据、或上游改过区划）。补一条把它原样显示出来
  // 并保持选中，而不是把用户存过的值悄悄抹掉 —— SchoolSelect 是同一条原则。
  const unknown = city !== null && !options.some((o) => o.value === city)

  const onProvinceChange = (code: number) => {
    onProvince(code)
    const list = citiesOf(code)
    // 只有一个市的省直接选中：四个直辖市加港澳台，共 7 个。对上海用户来说
    // 「市」这一级没有决策含量，为一件只有一个答案的事再点一次是纯摩擦。
    //
    // 但不隐藏市下拉 —— 控件凭空消失比预填更让人困惑，而且这么改之后
    // 「上海 → 上海」也是能在界面上被看见、被断言的。
    onCity(list.length === 1 ? list[0].code : null)
  }

  return (
    <div role="group" aria-labelledby={ariaLabelledby} className="grid grid-cols-2 gap-2">
      <Select
        id={`${idBase}_province`}
        numeric
        value={province}
        onChange={onProvinceChange}
        options={PROVINCE_OPTIONS}
        placeholder="省"
        disabled={disabled}
        aria-label="省份"
        aria-describedby={ariaDescribedby}
      />
      <Select
        id={`${idBase}_city`}
        numeric
        value={city}
        onChange={onCity}
        options={unknown ? [{ value: city, label: `未知城市 ${city}` }, ...options] : options}
        placeholder="市"
        // 省没选就没得可选。用原生 disabled 而不是 aria-disabled：
        // 前者会真的挡住操作，后者只是告诉读屏「它不能用」。
        disabled={disabled || cities.length === 0}
        aria-label="城市"
        aria-describedby={ariaDescribedby}
      />
    </div>
  )
}
