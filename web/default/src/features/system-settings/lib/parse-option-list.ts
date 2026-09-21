/**
 * 解析配置项里的 JSON 数组，并为缺省 id 的条目补 1-based id。
 * 配置值来自后端 option 字符串；损坏或非数组时返回空数组（不抛错）。
 */
export function parseOptionList<T extends { id?: number }>(data: string): T[] {
  try {
    const parsed = JSON.parse(data || '[]')
    if (!Array.isArray(parsed)) return []
    return parsed.map((item: T, idx: number) => ({
      ...item,
      id: item?.id || idx + 1,
    })) as T[]
  } catch {
    return []
  }
}
