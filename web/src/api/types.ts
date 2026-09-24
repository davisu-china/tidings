// 与后端 internal/handler + internal/service 里的 json tag 一一对应。
// 后端字段改名时这里必须同步改，否则只会在运行时炸。

/** 全站统一响应信封（handler.Response）。 */
export interface Envelope<T> {
  code: string
  message: string
  data?: T
}

/** 错误码是前后端契约的一部分，前端按 Code 分支，绝不解析 message。 */
export type ErrorCode =
  | 'INTERNAL'
  | 'BAD_REQUEST'
  | 'UNAUTHORIZED'
  | 'FORBIDDEN'
  | 'NOT_FOUND'
  | 'RATE_LIMITED'
  | 'TOKEN_EXPIRED'
  | 'INVALID_EMAIL'
  | 'EMAIL_REJECTED'
  | 'WEAK_PASSWORD'
  | 'PASSWORD_TOO_LONG'
  | 'EMAIL_TAKEN'
  | 'INVALID_CREDENTIALS'
  | 'ACCOUNT_LOCKED'
  | 'ACCOUNT_BANNED'
  | 'PROFILE_INCOMPLETE'
  | 'GENDER_IMMUTABLE'
  | 'FIELD_REJECTED'
  | 'UPLOAD_TOO_LARGE'
  | 'UPLOAD_BAD_TYPE'
  | 'PHOTO_LIMIT'
  // M3：引荐表态与会话
  | 'INTRO_NOT_FOUND'
  | 'INTRO_CLOSED'
  | 'INTRO_RESPONDED'
  | 'REASON_REQUIRED'
  | 'MATCH_NOT_FOUND'
  | 'BLOCKED'
  | 'MESSAGE_EMPTY'
  | 'MESSAGE_TOO_LONG'
  | 'WS_TICKET_INVALID'

/** 用户的下一步：onboarding 去建档，home 去首页。 */
export type NextStep = 'onboarding' | 'home'

/** users.status */
export type UserStatus =
  | 'onboarding'
  | 'active'
  | 'under_review'
  | 'banned'
  | 'deactivated'

export interface TokenPair {
  access_token: string
  refresh_token: string
  /** access token 剩余秒数 */
  expires_in: number
}

export interface LoginResult extends TokenPair {
  user_id: number
  next_step: NextStep
  is_new_user: boolean
}

export interface Me {
  user_id: number
  email: string
  status: UserStatus
  next_step: NextStep
  is_admin: boolean
  /** 仅管理员本人请求时下发，后端据此省掉一次手输 */
  admin_token?: string
}

/** 缺失的必填项，后端返回的字符串标识。 */
export type MissingField =
  | 'nickname'
  | 'gender'
  | 'birth_ym'
  | 'city_code'
  | 'height_cm'
  | 'education_level'
  | 'avatar_key'
  | 'photos'

/**
 * 建档入参。全部可选 —— 分步向导每步只提交自己那几个字段，
 * 后端用指针区分「这次没提交」和「提交了空值」。
 */
export interface ProfileInput {
  nickname?: string
  gender?: 'M' | 'F'
  /** YYYYMM，如 199508 */
  birth_ym?: number
  /** 出生日 1–31，与 birth_ym 配对提交。老档案只知道年月，所以是可选的 */
  birth_day?: number
  city_code?: number
  height_cm?: number
  education_level?: number

  hometown_code?: number
  weight_kg?: number
  school_name?: string
  occupation?: string
  company?: string
  income_band?: number
  smoking?: number
  drinking?: number
  /** 顿号分隔，≤6 个 */
  hobbies?: string
  intro?: string
  expectation?: string

  /** 婚育意愿 1 想要 / 2 不要 / 3 再说。这是我自己的情况，不是要求 */
  want_child?: number
  /** 婚史 1 未婚 / 2 离异。同上 */
  marital_status?: number
}

export interface Profile {
  user_id: number
  status: UserStatus
  next_step: NextStep
  completeness: number
  missing_required: MissingField[]

  nickname: string | null
  gender: 'M' | 'F' | null
  birth_ym: number | null
  /** 出生日。null = 只知道年月（老档案），不是「没填」 */
  birth_day: number | null
  age: number | null
  city_code: number | null
  height_cm: number | null
  education_level: number | null
  hometown_code: number | null
  weight_kg: number | null
  school_name: string
  occupation: string
  company: string
  income_band: number | null
  smoking: number | null
  drinking: number | null
  hobbies: string[]
  intro: string
  expectation: string
  want_child: number | null
  marital_status: number | null

  avatar_key: string
  avatar_url: string
  photo_count: number
}

export interface UploadTicket {
  upload_url: string
  object_key: string
  expires_in: number
  max_bytes: number
}

export interface Photo {
  id: number
  position: number
  object_key: string
  thumb_url: string
  card_url: string
  full_url: string
}

/**
 * 列表类响应外层是对象而不是裸数组（handler 里的 gin.H{"photos": ...}）。
 * 改动这里要连着 internal/handler/media.go 一起改。
 */
export interface PhotoList {
  photos: Photo[]
}

/**
 * 偏好（GET/PUT /me/preferences）。
 *
 * 全程「null = 不限」，这条约定要在三处一致：存储为空、打分时该维度
 * 不进分母、界面上显示「不限」。所以这里的字段一律可空，
 * 而不是用 0 或空串表示不限 —— 年龄下限 0 和「不限年龄」是两件事。
 */
export interface Preference {
  age_min: number | null
  age_max: number | null
  city_codes: number[]

  /** 我要求对方的婚育意愿 1 想要 / 2 不要 / 3 再说 */
  want_child: number | null
  /** 是否接受有婚史 0 不接受 / 1 接受 */
  accept_divorced: number | null
  /** 是否接受异地 1 接受 / 2 不接受 */
  accept_remote: number | null

  edu_min: number | null
  height_min: number | null
  height_max: number | null
  income_min: number | null
  income_max: number | null

  /** 有没有一行偏好记录。全「不限」时为 false，界面据此给引导 */
  configured: boolean
}

/** PUT 是整体替换：没传的字段即「不限」，没有「保持不变」这回事。 */
export type PreferenceInput = Omit<Preference, 'configured'>

/**
 * 防打扰设置（GET/PUT /me/settings）。
 *
 * push_frozen / unopened_streak 是用户改不了的，只读回显 ——
 * 推送被系统暂停却不说，用户只会以为推送坏了，然后去关通知权限，
 * 那一步之后就再也回不来了。
 */
export interface Settings {
  intros_paused: boolean
  /** 静默时段起止小时，0–23。落在区间内的推送推迟到结束之后再发 */
  quiet_start: number
  quiet_end: number

  push_frozen: boolean
  unopened_streak: number
}

export type SettingsInput = Pick<Settings, 'intros_paused' | 'quiet_start' | 'quiet_end'>

/**
 * 院校库的联想结果（GET /schools）。
 *
 * tier 是服务端按校名归一出来的层级（1 专科 / 2 普通本科 / 3 211 /
 * 4 985 / 5 QS 前 500），列表里不展示它 —— 它参与匹配打分，不该反过来
 * 变成用户挑学校的依据。
 */
export interface SchoolItem {
  name: string
  tier: number
}
