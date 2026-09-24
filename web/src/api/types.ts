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
  city_code?: number
  height_cm?: number
  education_level?: number

  hometown_code?: number
  weight_kg?: number
  school_name?: string
  occupation?: string
  company?: string
  income_band?: number
  chronotype?: number
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
  chronotype: number | null
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
