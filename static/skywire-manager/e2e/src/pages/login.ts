import {PATHS} from "../../../src/app/app-routing.module";
import BasePage from "./base-page.po";
import {findById} from "../util/selection";

export class LoginPage extends BasePage {

  path = PATHS.login;

  login()
  {
    findById('passwordInput').sendKeys('12341234');
    return findById('loginButton').click();
  }
}
