import {
  Button,
  Form,
  Input,
  Modal,
  Select,
  Space,
  Tooltip,
  Typography,
  Tag,
} from "antd";
import { useState } from "react";
import { MoreOutlined } from "@ant-design/icons";
import type { Page, Values, Node } from "./types";
export type Run = (name: string, values?: Values) => Promise<void>;
export const date = (value: string) =>
  value ? new Date(value).toLocaleString("ru-RU") : "—";
export function targets(p: Page) {
  return [
    { value: "direct", label: "Direct — провайдер" },
    { value: "proxy", label: "Выбранный VPN" },
    { value: "block", label: "Блокировать" },
    ...(p.Groups || []).map((g) => ({
      value: "group:" + g.id,
      label: "Группа: " + g.name,
    })),
    ...(p.Nodes || []).map((n) => ({
      value: "node:" + n.ID,
      label: n.Name,
      disabled: !!n.Compatibility,
    })),
  ];
}
export function Target({
  p,
  ...props
}: {
  p: Page;
  value?: string;
  onChange?: (value: string) => void;
  allowClear?: boolean;
}) {
  return (
    <Select
      showSearch
      optionFilterProp="label"
      options={targets(p)}
      {...props}
    />
  );
}
export function NodeLatency({ node }: { node: Node }) {
  return (
    <Tooltip title={(node.Probe?.Mode ? ({ tcp: "TCP", http: "HTTP", real: "Реальная задержка" }[node.Probe.Mode] || node.Probe.Mode) + ": " : "") + (node.Probe?.Message || "")}>
      <Tag
        color={
          node.Probe?.State === "ok"
            ? "green"
            : node.Probe?.State === "error"
              ? "red"
              : undefined
        }
      >
        {node.Probe?.State === "ok"
          ? node.Probe.HTTPSMS + " мс"
          : node.Probe?.State === "running"
            ? "Проверка…"
            : node.Probe?.State === "error"
              ? "Ошибка"
              : node.Probe?.State === "unsupported" ? "Неприменимо" : "—"}
      </Tag>
    </Tooltip>
  );
}
export function JsonDetails({ value }: { value: unknown }) {
  return (
    <pre className="data-pre">
      {typeof value === "string" ? value : JSON.stringify(value, null, 2)}
    </pre>
  );
}
export function Editor({
  title,
  open,
  onClose,
  initial,
  onSave,
  children,
}: {
  title: string;
  open: boolean;
  onClose: () => void;
  initial: Values;
  onSave: (v: Values) => Promise<void>;
  children: React.ReactNode;
}) {
  const [form] = Form.useForm();
  const [saving, setSaving] = useState(false),
    [error, setError] = useState("");
  return (
    <Modal
      title={title}
      open={open}
      onCancel={onClose}
      destroyOnHidden
      footer={null}
      width={680}
      afterOpenChange={(shown) => {
        if (shown) {
          form.setFieldsValue(initial);
          setError("");
        }
      }}
    >
      <Form
        form={form}
        layout="vertical"
        initialValues={initial}
        onFinish={async (v) => {
          setSaving(true);
          setError("");
          try {
            await onSave(v);
            onClose();
          } catch (e) {
            setError(e instanceof Error ? e.message : "Не удалось сохранить");
          } finally {
            setSaving(false);
          }
        }}
      >
        <Form.Item hidden name="id">
          <Input />
        </Form.Item>
        <Form.Item hidden name="group_id">
          <Input />
        </Form.Item>
        {children}
        {error && (
          <Typography.Paragraph type="danger">{error}</Typography.Paragraph>
        )}
        <Space>
          <Button type="primary" htmlType="submit" loading={saving}>
            Сохранить
          </Button>
          <Button onClick={onClose}>Отмена</Button>
        </Space>
      </Form>
    </Modal>
  );
}
export const required = [{ required: true, message: "Заполните поле" }];
export { MoreOutlined, Typography };
