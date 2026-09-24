import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'

/** 合并 className：clsx 处理条件，tailwind-merge 消掉互相打架的工具类。 */
export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs))
}
