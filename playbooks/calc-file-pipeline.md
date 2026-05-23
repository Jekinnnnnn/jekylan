---
name: calc-file-pipeline
description: File read → calc skill calculation → file write complete workflow. Reads data from a file, processes it through calc-step1/2/3 agents, and writes results back to a file.
when_to_use: When the user mentions "文件计算流程" or "calc file pipeline"
---

1. **Read input file**
   - prompt: "Use the `file_read` tool to read `input.txt`. The output format is `line_number<TAB>content` (e.g., `1\t10,20`). Ignore the line numbers and extract only the actual content after the tab. Then parse all numeric data from that content as parameters."
   - output: raw_data
   - confirm: true

2. **Data recognition** (agent: calc-step1)
   - prompt: "Recognize the following data: ${raw_data}"
   - output: recognized_data
   - confirm: true

3. **Preprocessing** (agent: calc-step2)
   - prompt: "Preprocess the following data: ${recognized_data}. Multiply each number by 10 and write the results to calc.txt."
   - output: preprocessed_data
   - confirm: true

4. **Format final output** (agent: calc-step3)
   - prompt: "Read calc.txt and format the final report."
   - output: calc_result
   - confirm: true
