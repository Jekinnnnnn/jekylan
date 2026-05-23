---
name: math-test
description: 加减乘除综合测试工作流：包含并行计算、串行计算、结果引用和每步确认
when_to_use: 当用户提到"测试计算链路"、"math test"、"加减乘除测试"时触发
---

- **并行加法** (agent: add)
  - prompt: "计算 100 + 50"
  - output: sum_result
  - confirm: true

- **并行乘法** (agent: multiply)
  - prompt: "计算 10 × 8"
  - output: prod_result
  - confirm: true

1. **减法：用加法结果减乘法结果** (agent: subtract)
   - prompt: "计算 ${sum_result} - ${prod_result}"
   - output: diff_result
   - confirm: true

2. **除法：用差值除以2** (agent: divide)
   - prompt: "计算 ${diff_result} ÷ 2"
   - output: final_result
   - confirm: true

3. **最终汇总** (agent: summary)
   - prompt: "汇总所有中间结果：加法=${sum_result}，乘法=${prod_result}，减法=${diff_result}，除法=${final_result}。请列出完整的计算链。"
   - confirm: true
