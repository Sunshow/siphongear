<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{ schema: Record<string, any>; modelValue: Record<string, any> }>()
const emit = defineEmits<{ (e: 'update:modelValue', v: Record<string, any>): void }>()

const value = computed({
  get: () => props.modelValue || {},
  set: (v) => emit('update:modelValue', v)
})

function setField(key: string, v: any) {
  emit('update:modelValue', { ...(props.modelValue || {}), [key]: v })
}

function fieldSchemaEntries(): Array<[string, any]> {
  if (!props.schema) return []
  return Object.entries(props.schema)
}

function asObjectText(v: any) {
  if (typeof v === 'string') return v
  return JSON.stringify(v ?? {}, null, 2)
}

function parseObject(key: string, text: string) {
  if (!text) { setField(key, {}); return }
  try { setField(key, JSON.parse(text)) } catch { /* keep typing */ setField(key, text) }
}

function asArrayValue(v: any) {
  return Array.isArray(v) ? v : []
}

function addArrayItem(key: string, schema: any) {
  const arr = asArrayValue(value.value[key]).slice()
  if (schema.itemSchema) {
    const empty: Record<string, any> = {}
    for (const k of Object.keys(schema.itemSchema)) empty[k] = ''
    arr.push(empty)
  } else {
    arr.push('')
  }
  setField(key, arr)
}

function removeArrayItem(key: string, idx: number) {
  const arr = asArrayValue(value.value[key]).slice()
  arr.splice(idx, 1)
  setField(key, arr)
}

function setArrayItem(key: string, idx: number, item: any) {
  const arr = asArrayValue(value.value[key]).slice()
  arr[idx] = item
  setField(key, arr)
}
</script>

<template>
  <el-form label-width="160px" label-position="left" size="small" v-if="schema && Object.keys(schema).length">
    <template v-for="[key, s] in fieldSchemaEntries()" :key="key">
      <el-form-item :label="s.label || key" :required="!!s.required">
        <template v-if="s.type === 'string' && !s.options">
          <div class="field-row">
            <el-input :model-value="value[key] ?? s.default ?? ''" @update:model-value="(v: string) => setField(key, v)" :placeholder="s.placeholder" class="field-input" />
            <el-text v-if="s.hint" type="info" size="small" class="field-hint">{{ s.hint }}</el-text>
          </div>
        </template>
        <template v-else-if="s.type === 'string' && s.options">
          <el-select :model-value="value[key] ?? s.default ?? ''" @update:model-value="(v: any) => setField(key, v)">
            <el-option v-for="o in s.options" :key="o" :label="o" :value="o" />
          </el-select>
        </template>
        <template v-else-if="s.type === 'number'">
          <el-input-number :model-value="Number(value[key] ?? s.default ?? 0)" @update:model-value="(v: any) => setField(key, v)" />
        </template>
        <template v-else-if="s.type === 'boolean'">
          <el-switch :model-value="value[key] ?? s.default ?? false" @update:model-value="(v: any) => setField(key, v)" />
        </template>
        <template v-else-if="s.type === 'text' || s.type === 'code'">
          <div class="field-row">
            <el-input
              type="textarea"
              :rows="s.type === 'code' ? 10 : 3"
              :model-value="value[key] ?? s.default ?? ''"
              @update:model-value="(v: any) => setField(key, v)"
              class="field-input"
            />
            <el-text v-if="s.hint" type="info" size="small" class="field-hint">{{ s.hint }}</el-text>
          </div>
        </template>
        <template v-else-if="s.type === 'object'">
          <el-input
            type="textarea"
            :rows="4"
            :model-value="asObjectText(value[key])"
            @update:model-value="(v: any) => parseObject(key, v)"
            placeholder='JSON object, e.g. {"k":"v"}'
          />
        </template>
        <template v-else-if="s.type === 'array'">
          <div style="width: 100%">
            <div v-for="(item, idx) in asArrayValue(value[key])" :key="idx" class="array-row">
              <template v-if="s.itemSchema">
                <el-row :gutter="6" style="flex:1">
                  <el-col :span="Math.floor(24 / Object.keys(s.itemSchema).length)" v-for="(sub, subKey) in s.itemSchema" :key="String(subKey)">
                    <el-input
                      :placeholder="(sub as any).label || String(subKey)"
                      :model-value="(item as any)?.[String(subKey)] ?? ''"
                      @update:model-value="(v: any) => setArrayItem(key, idx, { ...(item as any), [String(subKey)]: v })"
                    />
                  </el-col>
                </el-row>
              </template>
              <template v-else>
                <el-input
                  :model-value="String(item ?? '')"
                  @update:model-value="(v: any) => setArrayItem(key, idx, v)"
                />
              </template>
              <el-button size="small" type="danger" @click="removeArrayItem(key, idx)" style="margin-left: 6px">Delete</el-button>
            </div>
            <el-button size="small" @click="addArrayItem(key, s)">+ Add</el-button>
          </div>
        </template>
      </el-form-item>
    </template>
  </el-form>
  <el-empty v-else description="No configurable fields" :image-size="50" />
</template>

<style scoped>
.array-row { display: flex; align-items: flex-start; margin-bottom: 6px; }
.field-row { display: flex; align-items: flex-start; gap: 10px; width: 100%; }
.field-input { flex: 0 1 360px; }
.field-hint { flex: 1; line-height: 1.4; padding-top: 4px; }
@media (max-width: 768px) {
  .field-row { flex-direction: column; gap: 4px; }
  .field-input { flex: 1 1 auto; width: 100%; }
  .field-hint { flex: none; padding-top: 0; }
  .array-row { flex-direction: column; }
  .array-row :deep(.el-col) { flex: 0 0 100%; max-width: 100%; }
  .array-row :deep(.el-row) { width: 100%; row-gap: 6px; }
  .array-row :deep(.el-button) { margin-left: 0 !important; margin-top: 6px; align-self: flex-end; }
}
</style>
