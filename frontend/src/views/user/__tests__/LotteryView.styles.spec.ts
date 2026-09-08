import { describe, expect, it } from 'vitest'
import { compileStyle, parse } from 'vue/compiler-sfc'
import source from '../LotteryView.vue?raw'

describe('Lottery dark theme selectors', () => {
  it('keeps descendant selectors after Vue scoped CSS compilation', () => {
    const { descriptor } = parse(source)
    const style = descriptor.styles.find(block => block.scoped)
    expect(style).toBeDefined()
    const result = compileStyle({ source: style!.content, filename: 'LotteryView.vue', id: 'data-v-lottery', scoped: true })
    expect(result.errors).toEqual([])
    expect(result.code).toMatch(/\.dark \.lottery-stage\s*\{[^}]*#43212b/)
    expect(result.code).toMatch(/\.dark \.lottery-stage \.prize-tile\s*\{[^}]*#2b2229/)
    // 裸 .dark 会误改主题根节点，且奖品区域仍沿用浅色样式。
    expect(result.code).not.toMatch(/(?:^|\})\s*\.dark\s*\{/)
  })
})
