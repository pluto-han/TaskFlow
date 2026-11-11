import { useState } from "react";
import type { FieldErrors } from "../auth/validation";
export function PasswordFields({
  prefix,
  password,
  confirm,
  onPassword,
  onConfirm,
  errors,
  busy = false,
}: {
  prefix: string;
  password: string;
  confirm: string;
  onPassword(value: string): void;
  onConfirm(value: string): void;
  errors: FieldErrors;
  busy?: boolean;
}) {
  const [show, setShow] = useState(false);
  return (
    <>
      <div className="field">
        <label htmlFor={`${prefix}-password`}>New password</label>
        <input
          id={`${prefix}-password`}
          name="password"
          type={show ? "text" : "password"}
          autoComplete="new-password"
          value={password}
          onChange={(e) => onPassword(e.target.value)}
          disabled={busy}
          required
          aria-invalid={!!errors.password}
          aria-describedby={`${prefix}-password-help`}
        />
        <p
          id={`${prefix}-password-help`}
          className={errors.password ? "field-error" : "hint"}
        >
          {errors.password ||
            "Use 8–128 characters. A few memorable words work well."}
        </p>
      </div>
      <div className="field">
        <label htmlFor={`${prefix}-confirm`}>Confirm new password</label>
        <input
          id={`${prefix}-confirm`}
          name="confirm"
          type={show ? "text" : "password"}
          autoComplete="new-password"
          value={confirm}
          onChange={(e) => onConfirm(e.target.value)}
          disabled={busy}
          required
          aria-invalid={!!errors.confirm}
          aria-describedby={
            errors.confirm ? `${prefix}-confirm-error` : undefined
          }
        />
        {errors.confirm && (
          <p id={`${prefix}-confirm-error`} className="field-error">
            {errors.confirm}
          </p>
        )}
      </div>
      <label className="password-visibility">
        <input
          type="checkbox"
          checked={show}
          onChange={(e) => setShow(e.target.checked)}
          disabled={busy}
        />
        Show new password
      </label>
    </>
  );
}
