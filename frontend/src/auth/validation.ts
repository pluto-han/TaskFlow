export type FieldErrors = Partial<
  Record<"email" | "password" | "confirm", string>
>;

export function validate(
  email: string,
  password: string,
  confirm?: string,
): FieldErrors {
  const errors: FieldErrors = {};
  const normalized = email.trim();
  if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(normalized) || normalized.length > 254)
    errors.email = "请输入有效的邮箱地址。";
  if (confirm === undefined) {
    if (!password) errors.password = "请输入密码。";
  } else {
    const length = Array.from(password).length;
    if (length < 15 || length > 128 || !password.trim())
      errors.password = "密码需为 15–128 个字符，不能全为空格。";
    if (confirm !== password) errors.confirm = "两次输入的密码不一致。";
  }
  if (new TextEncoder().encode(password).length > 512)
    errors.password = "密码过长，请减少字符数量。";
  return errors;
}
